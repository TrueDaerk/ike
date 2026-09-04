// Package watch is the external-file-change service (Roadmap 0140): an
// fsnotify watcher over the project root, recursive, debounced, that reports
// coalesced changes as tea.Msgs via the host's Send. IKE's own writes are
// suppressed through a save epoch per path (MarkSaved), so a save never
// round-trips as an external change. For filesystems where fsnotify
// under-reports (network mounts), Poll drives an mtime+size comparison over
// explicitly tracked files behind the same message shape.
package watch

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// Kind classifies one filesystem event.
type Kind int

const (
	// FileChanged is a content change of a regular file.
	FileChanged Kind = iota
	// FileCreated is a new regular file.
	FileCreated
	// FileRemoved is a deleted (or renamed-away) regular file.
	FileRemoved
	// DirChanged is a directory whose entries changed (create/remove/rename
	// below it); the explorer refreshes from it.
	DirChanged
	// GitChanged is a change to the repository metadata under .git (commit,
	// branch switch, staging, pull — #738); the root model refreshes the VCS
	// snapshot from it. Path carries the .git directory.
	GitChanged
	// ConfigChanged is an external edit of the project settings file
	// (<root>/.ike/settings.toml, 0380 #795); the root model re-runs the
	// config reload pipeline from it. Path carries the settings file.
	ConfigChanged
)

// EventMsg is one debounced, coalesced filesystem event. The root model routes
// file kinds to the editor leaf owning Path and DirChanged to the explorer.
type EventMsg struct {
	Kind Kind
	Path string
}

// EventBatchMsg carries one debounce flush whole (#2176): every pending event
// of the window in one message, so a 300-file git checkout costs the app one
// Update pass instead of one per path. Events are sorted by path so a flush
// is deterministic; the paths are independent, so order carries no meaning.
type EventBatchMsg struct {
	Events []EventMsg
}

// debounceWindow coalesces bursts (editors write + rename, git checkouts).
const debounceWindow = 100 * time.Millisecond

// maxDebounceWait caps how long the sliding debounce may keep deferring a
// flush (#2163). A writer producing events faster than the window — a watch
// build, npm install, an agent rewriting the tree in a loop — used to reset
// the timer forever: no flush, an unboundedly growing pending map, and every
// external change invisible until the writer paused. Past this ceiling the
// pending timer is left to fire, so sustained churn still flushes at least
// once per ceiling-plus-window.
const maxDebounceWait = 10 * debounceWindow

// suppressWindow is how long after MarkSaved events for that path are treated
// as self-inflicted and dropped.
const suppressWindow = 500 * time.Millisecond

// Service owns the watcher goroutine and the debounce state.
type Service struct {
	send func(tea.Msg)

	mu      sync.Mutex
	epochs  map[string]time.Time // path -> last MarkSaved
	pending map[string]Kind      // debounced, coalesced events
	timer   *time.Timer
	armedAt time.Time            // when the current debounce cycle began (#2163)
	tracked map[string]fileStamp // poll fallback set
	w       *fsnotify.Watcher
	root    string

	// Per-path watches (#2506, extra.go): files outside the recursive root a
	// consumer asked for by name, reference-counted, plus the fsnotify
	// registrations currently backing them (the file itself, or its parent
	// directory while the file is missing).
	extras     map[string]int
	extraFiles map[string]bool
	extraDirs  map[string]bool

	debounce time.Duration
	maxWait  time.Duration    // flush-deferral ceiling (#2163); tests shrink it
	now      func() time.Time // injectable clock for tests

	// hashLimit is the largest file (bytes) the poll fallback content-hashes
	// (#149): above it a stamp carries no hash and mtime+size alone decide,
	// so a 50 MB log is never read wholesale just to confirm a touch. Zero
	// means no limit.
	hashLimit int64
}

// SetHashLimit caps poll-fallback content hashing at limit bytes (#149); zero
// removes the cap. Applies to stamps taken after the call.
func (s *Service) SetHashLimit(limit int64) {
	s.mu.Lock()
	s.hashLimit = limit
	s.mu.Unlock()
}

// fileStamp is the poll comparison state for one tracked file.
type fileStamp struct {
	mtime time.Time
	size  int64
	hash  string
}

// New returns a stopped Service that reports events through send (typically
// host.Send, which is a no-op until the program runs).
func New(send func(tea.Msg)) *Service {
	return &Service{
		send:       send,
		epochs:     map[string]time.Time{},
		pending:    map[string]Kind{},
		tracked:    map[string]fileStamp{},
		extras:     map[string]int{},
		extraFiles: map[string]bool{},
		extraDirs:  map[string]bool{},
		debounce:   debounceWindow,
		maxWait:    maxDebounceWait,
		now:        time.Now,
	}
}

// vendorNoiseDirs are non-dotted directory names never worth watching: they hold
// vendored dependencies or generated artefacts, exist in the thousands on large
// projects, and their churn is not an external edit the user cares about. Dotted
// directories (.git, .venv, .tox, .mypy_cache, …) are skipped separately, which
// is what tames the dominant Python case — all of site-packages lives under
// .venv/lib. Kept as a small deny-list rather than full gitignore parsing so the
// watch walk stays cheap and dependency-free (#596).
var vendorNoiseDirs = map[string]bool{
	"node_modules":  true,
	"__pycache__":   true,
	"site-packages": true,
	"vendor":        true,
}

// skipWatchDir reports whether a directory should be excluded from the recursive
// watch. It skips dot-prefixed directories (the project's convention everywhere —
// the explorer hides them, search ignores them) and the vendored-noise names.
// The watch root itself is always added before this filter runs, so a project
// living under a dotted path still works.
func skipWatchDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	return vendorNoiseDirs[name]
}

// Ignored reports whether path lies below a directory the recursive watch
// prunes — a dot-directory or a vendored-noise name (see skipWatchDir). It is
// the same rule Start walks with, exported so consumers that build lists out
// of watcher events (the change feed, #2000) hide exactly what the watcher
// would never have descended into, rather than re-deriving a second rule that
// drifts from this one.
//
// Only the directory segments *below* root are judged, mirroring Start's
// "the root itself is always watched" exception: a project living under
// ~/.config is not wholesale ignored. A path outside root is never ignored —
// this rule has nothing to say about it. An empty root judges every segment
// of the absolute path, which is what a caller without a watch root wants.
func Ignored(root, path string) bool {
	abs := absPath(path)
	rel := abs
	if root != "" {
		r, err := filepath.Rel(absPath(root), abs)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return false
		}
		rel = r
	}
	segs := strings.Split(filepath.ToSlash(rel), "/")
	for _, seg := range segs[:max(0, len(segs)-1)] { // the base name is a file, not a dir
		if seg == "" || seg == "." {
			continue
		}
		if skipWatchDir(seg) {
			return true
		}
	}
	return false
}

// TruncatedMsg reports that the recursive watch hit maxWatchDirs (#1011) and
// stopped adding directories: external changes below the unwatched remainder
// go unseen (open buffers stay covered by the poll fallback). Sent once per
// Start through the service's send seam so the app can toast it.
type TruncatedMsg struct{ Watched int }

// maxWatchDirs bounds the recursive watch (#1011): on macOS fsnotify's kqueue
// backend holds an open file descriptor per watched object, so pointing IKE
// at a huge root (a home directory via a stray restore, a monorepo) would
// exhaust the process fd limit before bubbletea can even create its input
// reader. Past the cap the walk stops; the root, .git and .ike watches are
// added regardless. A variable (not const) so tests can lower it.
var maxWatchDirs = 4096

// Start begins watching root recursively, skipping dot-directories and vendored
// noise (see skipWatchDir) and capping the watch count at maxWatchDirs
// (#1011). Idempotent per service: a running watcher is stopped first, which
// is also the project-switch (Roadmap 0090) restart path.
func (s *Service) Start(root string) error {
	s.Stop()
	// Normalize the root before anything registers (#1886): fsnotify reports
	// event paths exactly as watched, so a relative root ("." at startup)
	// yields relative event paths — and the .git/.ike classification below
	// matches on absolute markers. With relative paths every `git status`
	// echo (index.lock churn) fell through to the generic branch as a
	// FileRemoved + DirChanged, re-arming the VCS refresh that caused it: a
	// self-sustaining loop that pinned an idle session's CPU.
	root = absPath(root)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.w = w
	s.root = root
	s.mu.Unlock()
	watched := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		// A newer Start (project switch) superseded this walk (#2260): stop
		// registering on the closed watcher instead of walking the rest of
		// the tree for nothing. Start runs off the update loop now, so this
		// is a live possibility, not just a shutdown race.
		if !s.owns(w) {
			return filepath.SkipAll
		}
		// The root is always watched, even if its own name is dotted or noisy;
		// the filter only prunes descendants (#596).
		if path != root && skipWatchDir(d.Name()) {
			return filepath.SkipDir
		}
		if watched >= maxWatchDirs {
			return filepath.SkipAll
		}
		_ = w.Add(path)
		watched++
		return nil
	})
	if !s.owns(w) {
		// Superseded mid-walk: the newer Start owns the service now; close
		// this watcher (idempotent when Stop already did) and bow out without
		// touching the metadata watches or spawning a reader loop.
		_ = w.Close()
		return nil
	}
	if watched >= maxWatchDirs && s.send != nil {
		go s.send(TruncatedMsg{Watched: watched})
	}
	s.watchGitDir(root)
	s.watchConfigDir(root)
	// The per-path watches (#2506) survive a restart — a project switch must
	// not silently stop reporting the files an open diff is following — so
	// they re-register on the fresh watcher.
	s.armExtras()
	go s.loop(w)
	return nil
}

// owns reports whether w is still the service's active watcher — false once a
// later Start or a Stop replaced it.
func (s *Service) owns(w *fsnotify.Watcher) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w == w
}

// watchConfigDir adds the project config-directory watch (0380, #795):
// <root>/.ike holds settings.toml, whose external edits must reload the
// config. Not part of the recursive walk — skipWatchDir prunes dot dirs.
// A missing .ike is picked up by ingest when it is created later.
func (s *Service) watchConfigDir(root string) {
	dir := filepath.Join(root, ".ike")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w != nil {
		_ = s.w.Add(dir)
	}
}

// watchGitDir adds the repository metadata watches (#738): the .git directory
// and .git/logs. HEAD, index and packed-refs live at the .git top level, and
// the reflog (logs/HEAD) is appended by every commit, checkout, reset, merge
// and pull — so external git commands surface as GitChanged without watching
// the noisy objects tree. A .git *file* (linked worktree, submodule) is left
// unwatched. Not part of the recursive walk: skipWatchDir prunes dot dirs.
func (s *Service) watchGitDir(root string) {
	git := filepath.Join(root, ".git")
	if st, err := os.Stat(git); err != nil || !st.IsDir() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return
	}
	_ = s.w.Add(git)
	logs := filepath.Join(git, "logs")
	if st, err := os.Stat(logs); err == nil && st.IsDir() {
		_ = s.w.Add(logs)
	}
}

// Stop ends the watcher goroutine and cancels a pending debounce flush
// (#1001: an armed timer would otherwise fire once against the stopped
// service). Safe on a stopped service.
func (s *Service) Stop() {
	s.mu.Lock()
	w := s.w
	s.w = nil
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	// The closed watcher holds no registrations; the per-path *requests*
	// (s.extras) stay, so a later Start re-arms them (#2506).
	s.extraFiles, s.extraDirs = map[string]bool{}, map[string]bool{}
	s.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
}

// MarkSaved records that IKE itself just wrote path, so the watcher drops the
// resulting events instead of reporting a phantom external change.
func (s *Service) MarkSaved(path string) {
	abs := absPath(path)
	s.mu.Lock()
	s.epochs[abs] = s.now()
	_, isTracked := s.tracked[abs]
	s.mu.Unlock()
	if isTracked {
		s.Track(abs) // refresh the poll stamp so our own write never triggers
	}
}

// SavedRecently reports whether path's save epoch is inside the suppression
// window — i.e. an event for it right now would be treated as IKE's own write.
func (s *Service) SavedRecently(path string) bool {
	abs := absPath(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.epochs[abs]
	return ok && s.now().Sub(at) < suppressWindow
}

// loop drains fsnotify until the watcher closes.
func (s *Service) loop(w *fsnotify.Watcher) {
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			s.ingest(ev)
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

// ingest maps one raw fsnotify event onto the debounce state. The path is
// normalized up front (#1886): the .git/.ike classification matches on
// absolute markers, and misclassifying repository metadata as project files
// feeds the VCS refresh loop described on Start.
func (s *Service) ingest(ev fsnotify.Event) {
	path := absPath(ev.Name)
	if gitDir, ok := underGitDir(path); ok {
		s.ingestGit(ev, path, gitDir)
		return
	}
	if s.inConfigDir(path) {
		// Only the settings file matters under .ike (0380, #795): the layout,
		// session and usage stores churn on IKE's own writes and stay silent.
		if filepath.Base(path) == fileNameSettings &&
			(ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) || ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename)) {
			s.note(path, ConfigChanged)
		}
		return
	}
	if !covered(s.rootDir(), path) {
		// Not something the recursive root walk registered: either a
		// per-path watch (#2506) or a neighbour caught by one of its
		// directory fallbacks. ingestExtra decides and filters.
		s.ingestExtra(ev, path)
		return
	}
	switch {
	case ev.Has(fsnotify.Create):
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			// A new directory: watch it (unless it is vendored noise — e.g. a
			// mid-session `pip install` populating .venv, which would otherwise
			// start thousands of new watches, #596) and refresh its parent.
			// A freshly created <root>/.ike is the exception (#795): it is
			// watched so a first project-scope settings write is seen.
			if filepath.Base(path) == ".ike" && filepath.Dir(path) == s.rootDir() {
				s.mu.Lock()
				if s.w != nil {
					_ = s.w.Add(path)
				}
				s.mu.Unlock()
				return
			}
			if !skipWatchDir(filepath.Base(path)) {
				s.mu.Lock()
				if s.w != nil {
					_ = s.w.Add(path)
				}
				s.mu.Unlock()
			}
			s.note(filepath.Dir(path), DirChanged)
			return
		}
		s.note(path, FileCreated)
		s.note(filepath.Dir(path), DirChanged)
	case ev.Has(fsnotify.Remove), ev.Has(fsnotify.Rename):
		s.note(path, FileRemoved)
		s.note(filepath.Dir(path), DirChanged)
	case ev.Has(fsnotify.Write):
		s.note(path, FileChanged)
	}
}

// ingestGit maps one raw event under .git onto a coalesced GitChanged for the
// repo's .git directory (#738). Lock and temp files churn on every git command
// without signaling a state change, so they are dropped; a directory created
// directly under a watched git dir (a fresh repo growing .git/logs) is added
// to the watch so the reflog is covered from the first commit on.
func (s *Service) ingestGit(ev fsnotify.Event, path, gitDir string) {
	if !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Remove) &&
		!ev.Has(fsnotify.Rename) && !ev.Has(fsnotify.Write) {
		// Attribute-only churn (#1886): every `git status` — IKE's own VCS
		// refresh included — bumps the index's atime, which kqueue reports
		// as NOTE_ATTRIB (Chmod). No repository state changed, but mapping
		// it to GitChanged re-armed the very refresh that caused it: a
		// self-sustaining status loop that pinned an idle session's CPU on
		// every macOS project with a real .git directory. Real changes
		// always carry a Write/Create/Remove/Rename on an interior file
		// (index, HEAD, packed-refs, logs/HEAD), so attribute echoes are
		// safe to drop.
		return
	}
	if path == gitDir {
		// The git dir's own event: kqueue can report entry churn inside a
		// watched directory as a Write on the directory itself — index.lock
		// coming and going on every status run. The interior files carry
		// every real change, so the directory-self echo is dropped too;
		// classified as a generic FileChanged it would re-trigger the
		// refresh loop above through the app's VCS invalidation (#1886).
		return
	}
	base := filepath.Base(path)
	if strings.HasSuffix(base, ".lock") || strings.HasPrefix(base, "tmp_") {
		return
	}
	if ev.Has(fsnotify.Create) {
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			s.mu.Lock()
			if s.w != nil {
				_ = s.w.Add(path)
			}
			s.mu.Unlock()
			return
		}
	}
	s.note(gitDir, GitChanged)
}

// fileNameSettings is the settings file name inside <root>/.ike (mirrors
// internal/config; kept literal so watch stays dependency-free).
const fileNameSettings = "settings.toml"

// Root returns the directory the watcher last started on, empty while it has
// never run. Consumers filtering watcher-derived lists pass it to Ignored.
func (s *Service) Root() string {
	if s == nil {
		return ""
	}
	return s.rootDir()
}

// rootDir returns the current watch root.
func (s *Service) rootDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.root
}

// inConfigDir reports whether path lies directly inside the watch root's
// .ike directory (0380, #795).
func (s *Service) inConfigDir(path string) bool {
	return filepath.Dir(path) == filepath.Join(s.rootDir(), ".ike")
}

// underGitDir reports whether path lies inside a .git directory — or is one
// itself (#1886) — and returns that directory.
func underGitDir(path string) (string, bool) {
	marker := string(filepath.Separator) + ".git" + string(filepath.Separator)
	if idx := strings.Index(path, marker); idx >= 0 {
		return path[:idx+len(marker)-1], true
	}
	if filepath.Base(path) == ".git" {
		return path, true
	}
	return "", false
}

// note records one coalesced event and (re)arms the debounce flush. It is the
// single entry point for raw events — fsnotify and the poll fallback share it.
func (s *Service) note(path string, kind Kind) {
	path = absPath(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind != DirChanged && kind != FileRemoved {
		if at, ok := s.epochs[path]; ok && s.now().Sub(at) < suppressWindow {
			return // IKE's own save echoing back
		}
	}
	if old, ok := s.pending[path]; ok {
		kind = mergeKinds(old, kind)
	}
	s.pending[path] = kind
	if s.timer == nil {
		s.timer = time.AfterFunc(s.debounce, s.flush)
		s.armedAt = s.now()
	} else if s.now().Sub(s.armedAt) < s.maxWait {
		s.timer.Reset(s.debounce)
	}
	// Past maxWait the running timer is left alone: it fires within
	// one window of its last reset, so sustained churn cannot defer the
	// flush forever (#2163).
}

// mergeKinds coalesces a new raw kind onto an existing pending one: removal
// wins, creation survives follow-up writes, everything else keeps the latest.
func mergeKinds(old, next Kind) Kind {
	switch {
	case next == FileRemoved || old == FileRemoved:
		return FileRemoved
	case old == FileCreated && next == FileChanged:
		return FileCreated
	}
	return next
}

// flush emits every pending event as one EventBatchMsg (#2176) — one Update
// pass per debounce window regardless of how many paths changed. Suppression is
// re-checked here (#1406): the editor writes first and stamps MarkSaved a few
// milliseconds later, and on macOS fsnotify routinely delivers the write event
// inside that gap — at note time the epoch is not stamped yet, so the event
// slips into pending. By flush time (one debounce later) the stamp has landed,
// so the own-write is recognized and dropped.
func (s *Service) flush() {
	s.mu.Lock()
	batch := s.pending
	s.pending = map[string]Kind{}
	s.timer = nil
	send := s.send
	now := s.now()
	for path, kind := range batch {
		if kind == DirChanged || kind == FileRemoved {
			continue
		}
		if at, ok := s.epochs[path]; ok && now.Sub(at) < suppressWindow {
			delete(batch, path)
		}
	}
	// Expired epochs are dead weight — the suppression window is long past —
	// so each flush sweeps them out (#1562); without this the map grew one
	// entry per path ever saved for the session's lifetime.
	for path, at := range s.epochs {
		if now.Sub(at) >= suppressWindow {
			delete(s.epochs, path)
		}
	}
	s.mu.Unlock()
	// A per-path watch whose file was just replaced or re-created re-arms
	// here (#2506): the flush is the one point every event kind passes.
	s.armExtras()
	if send == nil || len(batch) == 0 {
		return
	}
	events := make([]EventMsg, 0, len(batch))
	for path, kind := range batch {
		events = append(events, EventMsg{Kind: kind, Path: path})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Path < events[j].Path })
	send(EventBatchMsg{Events: events})
}

// absPath normalises a path for epoch/tracking lookups.
func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

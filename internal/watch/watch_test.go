package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// collector gathers sent messages behind a mutex (flush runs on a timer
// goroutine) and lets tests wait for a batch.
type collector struct {
	mu   sync.Mutex
	msgs []EventMsg
	raw  []tea.Msg // every msg, for non-event types (TruncatedMsg, #1011)
}

func (c *collector) send(msg tea.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raw = append(c.raw, msg)
	if b, ok := msg.(EventBatchMsg); ok {
		// One flush = one batch (#2176); tests still assert per-event.
		c.msgs = append(c.msgs, b.Events...)
	}
}

func (c *collector) wait(t *testing.T, n int) []EventMsg {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if len(c.msgs) >= n {
			out := append([]EventMsg(nil), c.msgs...)
			c.mu.Unlock()
			return out
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.Fatalf("timed out waiting for %d events, have %v", n, c.msgs)
	return nil
}

func (c *collector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.msgs)
}

// service returns a fast-debounce Service wired to a collector, bypassing
// fsnotify: tests feed raw events through note() like ingest would.
func service() (*Service, *collector) {
	c := &collector{}
	s := New(c.send)
	s.debounce = 10 * time.Millisecond
	return s, c
}

func TestDebounceCoalescesBursts(t *testing.T) {
	s, c := service()
	for i := 0; i < 5; i++ {
		s.note("/p/a.go", FileChanged)
	}
	got := c.wait(t, 1)
	if len(got) != 1 || got[0].Kind != FileChanged {
		t.Fatalf("5 writes must coalesce to one FileChanged, got %v", got)
	}
}

func TestMergeCreateThenWriteStaysCreated(t *testing.T) {
	s, c := service()
	s.note("/p/new.go", FileCreated)
	s.note("/p/new.go", FileChanged)
	s.note("/p/gone.go", FileChanged)
	s.note("/p/gone.go", FileRemoved)
	got := c.wait(t, 2)
	kinds := map[string]Kind{}
	for _, ev := range got {
		kinds[filepath.Base(ev.Path)] = ev.Kind
	}
	if kinds["new.go"] != FileCreated {
		t.Fatalf("create+write must stay FileCreated, got %v", kinds["new.go"])
	}
	if kinds["gone.go"] != FileRemoved {
		t.Fatalf("write+remove must end FileRemoved, got %v", kinds["gone.go"])
	}
}

func TestSelfEventSuppression(t *testing.T) {
	s, c := service()
	s.MarkSaved("/p/mine.go")
	s.note("/p/mine.go", FileChanged)
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("own save must be suppressed, got %d events", n)
	}
	// Outside the suppression window the same path reports again.
	s.now = func() time.Time { return time.Now().Add(suppressWindow + time.Second) }
	s.note("/p/mine.go", FileChanged)
	if got := c.wait(t, 1); got[0].Kind != FileChanged {
		t.Fatalf("post-window change must report, got %v", got)
	}
}

func TestMarkSavedAfterNoteStillSuppresses(t *testing.T) {
	// The write event routinely beats MarkSaved into note() (#1406): the
	// editor writes the file first and stamps the epoch a few milliseconds
	// later. The flush-time re-check must still drop the pending event.
	s, c := service()
	s.note("/p/mine.go", FileChanged)
	s.MarkSaved("/p/mine.go")
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("own save noted before MarkSaved must still be suppressed, got %d events", n)
	}
	// A removal pending at flush time is never dropped: our own saves do not
	// remove files, so the event is genuinely external.
	s.note("/p/mine.go", FileRemoved)
	if got := c.wait(t, 1); got[0].Kind != FileRemoved {
		t.Fatalf("removal must survive the flush re-check, got %v", got)
	}
}

func TestFsnotifyEndToEnd(t *testing.T) {
	dir := t.TempDir()
	s, c := service()
	if err := s.Start(dir); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	found := false
	for _, ev := range got {
		if ev.Path == path && (ev.Kind == FileCreated || ev.Kind == FileChanged) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a create/change for %s, got %v", path, got)
	}
}

// TestGitMetadataEvents guards #738: changes under .git (index, HEAD, the
// reflog) surface as one coalesced GitChanged so external git commands —
// commits in a lazygit pane, a terminal checkout — refresh the VCS snapshot.
// Lock-file churn stays silent.
func TestGitMetadataEvents(t *testing.T) {
	dir := t.TempDir()
	git := filepath.Join(dir, ".git")
	if err := os.MkdirAll(filepath.Join(git, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	if err := s.Start(dir); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	// index.lock churn must not surface.
	if err := os.WriteFile(filepath.Join(git, "index.lock"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("lock file must stay silent, got %d events", n)
	}

	// A staged change (index) and a commit (reflog append) coalesce to one
	// GitChanged for the .git dir.
	if err := os.WriteFile(filepath.Join(git, "index"), []byte("idx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(git, "logs", "HEAD"), []byte("reflog"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	if len(got) != 1 || got[0].Kind != GitChanged || got[0].Path != git {
		t.Fatalf("want one GitChanged for %s, got %v", git, got)
	}
}

// TestIngestGitMapsEvents covers the raw mapping without a live watcher.
func TestIngestGitMapsEvents(t *testing.T) {
	s, c := service()
	git := filepath.Join(string(filepath.Separator)+"repo", ".git")
	s.ingest(fsnotify.Event{Name: filepath.Join(git, "HEAD"), Op: fsnotify.Write})
	s.ingest(fsnotify.Event{Name: filepath.Join(git, "refs", "heads", "main"), Op: fsnotify.Write})
	s.ingest(fsnotify.Event{Name: filepath.Join(git, "index.lock"), Op: fsnotify.Create})
	s.ingest(fsnotify.Event{Name: filepath.Join(git, "objects", "tmp_obj_x"), Op: fsnotify.Create})
	got := c.wait(t, 1)
	if len(got) != 1 || got[0].Kind != GitChanged || got[0].Path != git {
		t.Fatalf("want one GitChanged for %s, got %v", git, got)
	}
}

// TestGitStatusEchoesStaySilent guards #1886: every `git status` run — IKE's
// own VCS refresh included — bumps .git/index's atime (kqueue NOTE_ATTRIB →
// Chmod) and can echo entry churn as a Write on the .git directory itself.
// Mapped to GitChanged those echoes re-armed the very refresh that caused
// them, a self-sustaining status loop pinning an idle session's CPU. Neither
// may produce an EventMsg, while real interior changes still surface.
func TestGitStatusEchoesStaySilent(t *testing.T) {
	s, c := service()
	git := filepath.Join(string(filepath.Separator)+"repo", ".git")
	// The full echo set of one `git status` run on macOS: index atime bump,
	// lock churn, directory-self write.
	s.ingest(fsnotify.Event{Name: filepath.Join(git, "index"), Op: fsnotify.Chmod})
	s.ingest(fsnotify.Event{Name: filepath.Join(git, "index.lock"), Op: fsnotify.Create})
	s.ingest(fsnotify.Event{Name: filepath.Join(git, "index.lock"), Op: fsnotify.Remove})
	s.ingest(fsnotify.Event{Name: git, Op: fsnotify.Write})
	s.ingest(fsnotify.Event{Name: git, Op: fsnotify.Chmod})
	time.Sleep(50 * time.Millisecond) // past the 10ms test debounce
	if n := c.count(); n != 0 {
		t.Fatalf("git status echoes must stay silent, got %d events: %v", n, c.msgs)
	}
	// Interior content events still surface: the loop guards must not mute
	// real repository changes (a staged file rewriting the index).
	s.ingest(fsnotify.Event{Name: filepath.Join(git, "index"), Op: fsnotify.Create})
	got := c.wait(t, 1)
	if len(got) != 1 || got[0].Kind != GitChanged || got[0].Path != git {
		t.Fatalf("want one GitChanged for %s, got %v", git, got)
	}
}

// TestRelativeRootClassifiesGitDir guards the live-app half of #1886: main
// starts the watcher on "." — and fsnotify reports event paths exactly as
// watched, so with a relative root the ".git" classification (which matches
// on absolute markers) never applied. Every `git status` echo then surfaced
// as a generic FileRemoved + DirChanged, re-arming the VCS refresh that
// caused it. A relative root must classify .git events exactly like an
// absolute one: lock churn silent, real changes as GitChanged.
func TestRelativeRootClassifiesGitDir(t *testing.T) {
	dir := t.TempDir()
	git := filepath.Join(dir, ".git")
	if err := os.MkdirAll(git, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	s, c := service()
	if err := s.Start("."); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	// The status-run echo: lock file coming and going must stay silent.
	if err := os.WriteFile(filepath.Join(git, "index.lock"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(git, "index.lock")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("lock churn under a relative root must stay silent, got %v", c.msgs)
	}

	// A real metadata change still surfaces, with the absolute git dir path.
	if err := os.WriteFile(filepath.Join(git, "index"), []byte("idx"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	want := absPath(git)
	if len(got) != 1 || got[0].Kind != GitChanged || got[0].Path != want {
		t.Fatalf("want one GitChanged for %s, got %v", want, got)
	}
}

func TestSkipWatchDir(t *testing.T) {
	skip := []string{".git", ".venv", ".tox", ".mypy_cache", ".idea", "node_modules", "__pycache__", "site-packages", "vendor"}
	for _, n := range skip {
		if !skipWatchDir(n) {
			t.Errorf("skipWatchDir(%q) = false, want true", n)
		}
	}
	keep := []string{"src", "internal", "cmd", "app", "lib", "my_package"}
	for _, n := range keep {
		if skipWatchDir(n) {
			t.Errorf("skipWatchDir(%q) = true, want false", n)
		}
	}
}

// TestStartSkipsVendorDirs verifies the recursive watch prunes vendored/noise
// subtrees (#596): a write deep inside node_modules produces no event, while a
// normal source file still does. This is the mechanism that stops a populated
// .venv / node_modules from registering thousands of watches and flooding the
// event loop.
func TestStartSkipsVendorDirs(t *testing.T) {
	dir := t.TempDir()
	noise := filepath.Join(dir, "node_modules", "pkg")
	src := filepath.Join(dir, "src")
	for _, d := range []string{noise, src} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s, c := service()
	if err := s.Start(dir); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	// A write inside the pruned subtree must not be reported (its dir is unwatched).
	if err := os.WriteFile(filepath.Join(noise, "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A write in a watched source dir must be reported; waiting for it also
	// gives the (unwanted) node_modules event time to arrive if it were coming.
	srcFile := filepath.Join(src, "main.go")
	if err := os.WriteFile(srcFile, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	for _, ev := range got {
		if filepath.Dir(ev.Path) == noise || filepath.Base(filepath.Dir(ev.Path)) == "pkg" {
			t.Fatalf("got an event for a pruned vendored path: %v", ev)
		}
	}
	sawSrc := false
	for _, ev := range got {
		if ev.Path == srcFile {
			sawSrc = true
		}
	}
	if !sawSrc {
		t.Fatalf("expected an event for %s, got %v", srcFile, got)
	}
}

func TestPollDetectsChangeAndIgnoresTouch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(path, []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	s.Track(path)

	// A bare touch (new mtime, same size + content) must not report.
	if err := os.Chtimes(path, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.Poll()
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("touch must not report, got %d events", n)
	}

	// A real same-size edit is caught by the hash-on-suspicion path.
	if err := os.WriteFile(path, []byte("bbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Poll()
	got := c.wait(t, 1)
	if got[0].Kind != FileChanged || got[0].Path != path {
		t.Fatalf("same-size edit must report FileChanged, got %v", got)
	}

	// Removal reports and drops tracking.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.Poll()
	got = c.wait(t, 2)
	if got[len(got)-1].Kind != FileRemoved {
		t.Fatalf("removal must report FileRemoved, got %v", got)
	}
}

func TestMarkSavedRefreshesPollStamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	s.Track(path)
	// IKE writes the file itself and stamps the epoch: Poll stays silent.
	if err := os.WriteFile(path, []byte("v2!"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.MarkSaved(path)
	s.Poll()
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("own save must not poll-report, got %d events", n)
	}
}

func TestPollLargeFileSkipsHashUsesMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	s.SetHashLimit(4) // 10-byte file is over the limit: never content-hashed
	s.Track(path)

	if st := s.tracked[absPath(path)]; st.hash != "" {
		t.Fatalf("large file must not be hashed, got %q", st.hash)
	}

	// mtime+size decide (#149): a bare touch reports for a large file — the
	// conservative reload beats reading megabytes to rule it out.
	if err := os.Chtimes(path, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.Poll()
	got := c.wait(t, 1)
	if got[0].Kind != FileChanged || got[0].Path != path {
		t.Fatalf("mtime change on a large file must report, got %v", got)
	}
}

func TestPollSmallFileStillHashedUnderLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.txt")
	if err := os.WriteFile(path, []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	s.SetHashLimit(1024)
	s.Track(path)
	// Under the limit the hash-on-suspicion path still absorbs a bare touch.
	if err := os.Chtimes(path, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.Poll()
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("touch under the hash limit must not report, got %d events", n)
	}
}

// TestConfigFileEvents (0380, #795): an external edit of
// <root>/.ike/settings.toml surfaces as ConfigChanged; sibling state stores
// (layout.json) stay silent, and a late-created .ike directory is picked up.
func TestConfigFileEvents(t *testing.T) {
	dir := t.TempDir()
	ike := filepath.Join(dir, ".ike")
	if err := os.Mkdir(ike, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(ike, "settings.toml")
	if err := os.WriteFile(settings, []byte("[editor]\ntab_width = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	if err := s.Start(dir); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	// State-store churn under .ike stays silent.
	if err := os.WriteFile(filepath.Join(ike, "layout.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := c.count(); n != 0 {
		t.Fatalf("layout.json churn must stay silent, got %d events", n)
	}

	if err := os.WriteFile(settings, []byte("[editor]\ntab_width = 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	if got[0].Kind != ConfigChanged || got[0].Path != settings {
		t.Fatalf("settings edit must surface as ConfigChanged, got %+v", got)
	}
}

// TestConfigDirCreatedLate (#795): a project without .ike gains one (the
// first project-scope write); the new directory is watched so the settings
// file's first edit is seen.
func TestConfigDirCreatedLate(t *testing.T) {
	dir := t.TempDir()
	s, c := service()
	if err := s.Start(dir); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	ike := filepath.Join(dir, ".ike")
	if err := os.Mkdir(ike, 0o755); err != nil {
		t.Fatal(err)
	}
	// Give fsnotify a beat to register the new watch before writing.
	time.Sleep(50 * time.Millisecond)
	settings := filepath.Join(ike, "settings.toml")
	if err := os.WriteFile(settings, []byte("[editor]\ntab_width = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	found := false
	for _, ev := range got {
		if ev.Kind == ConfigChanged && ev.Path == settings {
			found = true
		}
	}
	if !found {
		t.Fatalf("settings write in a late .ike must surface, got %+v", got)
	}
}

// TestStartCapsWatchCount guards #1011: the recursive walk stops at
// maxWatchDirs and reports the truncation once; small roots stay silent.
func TestStartCapsWatchCount(t *testing.T) {
	old := maxWatchDirs
	maxWatchDirs = 4
	defer func() { maxWatchDirs = old }()

	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		if err := os.MkdirAll(filepath.Join(dir, fmt.Sprintf("d%02d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s, c := service()
	if err := s.Start(dir); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		var hit *TruncatedMsg
		for _, m := range c.raw {
			if tm, ok := m.(TruncatedMsg); ok {
				hit = &tm
			}
		}
		c.mu.Unlock()
		if hit != nil {
			if hit.Watched != 4 {
				t.Fatalf("Watched=%d want 4", hit.Watched)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cap hit must send a TruncatedMsg")
}

// TestStartSmallRootNoTruncation: below the cap no TruncatedMsg is sent.
func TestStartSmallRootNoTruncation(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	if err := s.Start(dir); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(50 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.raw {
		if _, ok := m.(TruncatedMsg); ok {
			t.Fatal("small root must not report truncation")
		}
	}
}

// TestUntrackPrunesEpoch (#1562): Untrack drops the save epoch with the poll
// stamp, so the maps never outgrow the open-file set.
func TestUntrackPrunesEpoch(t *testing.T) {
	s := New(nil)
	path := filepath.Join(t.TempDir(), "a.txt")
	s.Track(path)
	s.MarkSaved(path)
	if !s.SavedRecently(path) {
		t.Fatal("MarkSaved must register the epoch")
	}
	s.Untrack(path)
	s.mu.Lock()
	nTracked, nEpochs := len(s.tracked), len(s.epochs)
	s.mu.Unlock()
	if nTracked != 0 || nEpochs != 0 {
		t.Fatalf("after Untrack: tracked=%d epochs=%d, want 0/0", nTracked, nEpochs)
	}
}

// TestFlushSweepsExpiredEpochs (#1562): each debounce flush removes epochs
// past the suppression window.
func TestFlushSweepsExpiredEpochs(t *testing.T) {
	s := New(nil)
	s.MarkSaved(filepath.Join(t.TempDir(), "old.txt"))
	s.now = func() time.Time { return time.Now().Add(suppressWindow + time.Second) }
	s.flush()
	s.mu.Lock()
	n := len(s.epochs)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("expired epochs after flush = %d, want 0", n)
	}
}

// TestFlushSendsOneBatch (#2176): a multi-path debounce window arrives as one
// EventBatchMsg — one Update pass for the whole checkout — sorted by path.
func TestFlushSendsOneBatch(t *testing.T) {
	s, c := service()
	s.note("/p/b.go", FileChanged)
	s.note("/p/a.go", FileCreated)
	s.note("/p/c.go", FileChanged)
	c.wait(t, 3)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.raw) != 1 {
		t.Fatalf("raw messages = %d, want 1 batch", len(c.raw))
	}
	b, ok := c.raw[0].(EventBatchMsg)
	if !ok || len(b.Events) != 3 {
		t.Fatalf("msg = %#v, want a 3-event batch", c.raw[0])
	}
	for i, want := range []string{"/p/a.go", "/p/b.go", "/p/c.go"} {
		if b.Events[i].Path != want {
			t.Fatalf("batch order = %v, want sorted by path", b.Events)
		}
	}
}

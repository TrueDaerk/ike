package complexity

// index.go is the project-wide complexity index (Roadmap 0550, #2916): every
// supported file under the project root measured once in the background and
// kept current from watcher events and buffer saves. It wraps the shared
// per-language project walk (internal/complete/langindex) the PHP
// declaration index runs on: the lazy walk with its skip rules and size
// caps, the single re-extraction worker, and a settle-timed change callback.
// Consumers — the tool window, the Problems feed, the status segment — read
// an immutable Snapshot and never the walk's internals.
//
// The walk is keyed by language, but the index wants one pass over every
// file of every language with a spec, so it registers a single pseudo
// language (indexLang): the classifier claims a path for it when lang.ByPath
// names a language with a lang.ComplexitySpec and no complexity.exclude glob
// matches, and the extractor re-derives the real language from the path.
// Nothing else is read.

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ike/internal/complete/langindex"
	"ike/internal/lang"
	"ike/internal/pathglob"
)

// indexLang is the pseudo language id the single walk runs under.
const indexLang = "complexity"

// Walk bounds: generated code and bundles run long, so the per-file cap sits
// above the symbol index's; the file cap keeps a monorepo from pinning a
// core for minutes.
const (
	indexMaxFileSize = 512 << 10
	indexMaxFiles    = 50000
	// changeSettle paces the content-change callback: it collapses a burst
	// of re-extractions (a branch switch touching hundreds of files) into
	// one notification, so Gen moves once per settle window.
	changeSettle = 250 * time.Millisecond
	// changeWatch is how long the settle poll keeps looking after an event
	// whose work lands asynchronously (a re-extraction queued behind the
	// worker, a scan still running).
	changeWatch = 2 * time.Second
)

// Options are the [complexity] settings the index runs under. Enabled is
// the master switch (complexity.enabled): off stops the walk and drops the
// snapshot. Exclude is the complexity.exclude glob list, matched against the
// project-relative slash path (`vendor/**`, `**/*_test.go`) and, for a
// pattern without a slash, against the base name too (`*_test.go`).
type Options struct {
	Enabled bool
	Exclude []string
}

// equal reports whether o and p configure the same walk.
func (o Options) equal(p Options) bool {
	if o.Enabled != p.Enabled || len(o.Exclude) != len(p.Exclude) {
		return false
	}
	for i := range o.Exclude {
		if o.Exclude[i] != p.Exclude[i] {
			return false
		}
	}
	return true
}

// View is an immutable snapshot of the index: Gen is the notification
// generation it was taken at (the Gen of the last ComplexityChangedMsg, or
// a later one), Files the measured functions per absolute path. A consumer
// keeps a View for as long as it likes; the index never mutates one.
type View struct {
	Gen   int
	Files map[string][]Function
}

// Paths lists the snapshot's files, sorted.
func (v View) Paths() []string {
	out := make([]string, 0, len(v.Files))
	for p := range v.Files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Functions counts the measured units across the snapshot.
func (v View) Functions() int {
	n := 0
	for _, fns := range v.Files {
		n += len(fns)
	}
	return n
}

// Summary aggregates every function of the snapshot, each file judged by
// the thresholds res resolves for it (see SummarizeFiles).
func (v View) Summary(res Resolver) Summary { return SummarizeFiles(v.Files, res) }

// Stats describes the index for a status report and the complexity.scan
// telemetry op: no path travels.
type Stats struct {
	// Enabled mirrors complexity.enabled; a disabled index holds nothing.
	Enabled bool
	// Unavailable is set when the build has no syntax tree for any language
	// with a spec (no cgo): the index can never fill.
	Unavailable bool
	// Scanning is true while the project walk runs.
	Scanning bool
	// Files counts measured files, Functions the units in them.
	Files     int
	Functions int
	// LastScan is the walk's duration; Truncated says it stopped at the file
	// cap before the tree was exhausted.
	LastScan  time.Duration
	Truncated bool
	// Cached counts the files the finished walk took from the persisted
	// cache instead of measuring them (#2967); CacheCorrupt says the cache
	// it tried to seed from was damaged (ignored, and rewritten by the walk).
	Cached       int
	CacheCorrupt bool
}

// Index is the project complexity index. Zero value is not usable; use
// NewIndex.
type Index struct {
	mu      sync.Mutex
	root    string
	opts    Options
	started bool
	project *langindex.Index[[]Function] // nil while disabled
	// gen is the notification generation (View.Gen): it moves once per
	// settle window in which the walk's content changed, and on every
	// drop or rebuild.
	gen int
	// view caches the snapshot derived for viewKey; a moved key rebuilds
	// it lazily on the next Snapshot.
	view    *View
	viewKey viewKey

	onChange   func(gen int)
	chTimer    *time.Timer
	lastGen    viewKey
	watchUntil time.Time
	// pending forces the next settle check to count as a change: a drop or
	// a rebuild replaces the content even when the walk's key did not move
	// (an enable + disable inside one settle window).
	pending bool

	onScan  func(Stats)
	scanned *langindex.Index[[]Function]

	// cacheFile is where the walk persists (CacheFile(root), #2967);
	// corrupt names the walk whose seed load found it damaged.
	cacheFile string
	corrupt   *langindex.Index[[]Function]
}

// viewKey identifies the content a snapshot was built from.
type viewKey struct {
	projGen uint64
	project *langindex.Index[[]Function]
}

// NewIndex returns the index over the project at root ("" indexes nothing)
// under opts. The walk does not start before Start: the app calls it from
// Init, after the program's sender is wired, like the TODO index.
func NewIndex(root string, opts Options) *Index {
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
	}
	x := &Index{root: root, opts: opts, cacheFile: CacheFile(root)}
	if opts.Enabled {
		x.project = x.newProject(opts, true)
	}
	return x
}

// Root is the project root the index walks.
func (x *Index) Root() string { return x.root }

// Options returns the settings the index currently runs under.
func (x *Index) Options() Options {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.opts
}

// Start begins the project walk in the background (once; later calls are
// no-ops). An index built disabled starts the moment it is enabled.
func (x *Index) Start() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.started = true
	x.ensureLocked()
}

// ensureLocked kicks off the walk when the index is started and enabled.
func (x *Index) ensureLocked() {
	if !x.started || x.project == nil {
		return
	}
	x.project.Ensure(indexLang)
	x.armWatchLocked()
}

// Reconfigure applies changed [complexity] settings live: turning the
// master switch off drops the index and bumps the generation (consumers see
// an empty snapshot), turning it on builds and walks it, a changed exclude
// list rebuilds. Unchanged options are a no-op. Turning the index on seeds
// the walk from the persisted cache; a changed exclude list walks cold.
func (x *Index) Reconfigure(opts Options) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.opts.equal(opts) {
		return
	}
	x.opts = opts
	x.view = nil
	x.pending = true
	if !opts.Enabled {
		x.project = nil
		x.armChangeLocked()
		return
	}
	x.project = x.newProject(opts, x.project == nil)
	x.ensureLocked()
}

// Rebuild drops the walk and starts a fresh one: the escape hatch for a
// tree that changed underneath ike without watcher events. The rebuild
// ignores the persisted cache (#2967) — it is the escape hatch for a cache
// gone wrong too — and replaces it once the walk finishes. It reports false
// when there is nothing to rebuild (disabled, or no syntax tree at all).
func (x *Index) Rebuild() bool {
	if !x.Available() {
		return false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.opts.Enabled {
		return false
	}
	x.project = x.newProject(x.opts, false)
	x.view = nil
	x.pending = true
	x.ensureLocked()
	return true
}

// newProject builds the walk for opts. The walk persists through the cache
// (#2967) once it finishes and seeds itself from it unless seeded is false
// (a rebuild, a changed exclude list). The caller holds x.mu.
func (x *Index) newProject(opts Options, seeded bool) *langindex.Index[[]Function] {
	limits := langindex.Limits{MaxFileSize: indexMaxFileSize, MaxFiles: indexMaxFiles}
	root := x.root
	exclude := append([]string(nil), opts.Exclude...)
	langOf := func(path string) string { return classify(root, exclude, path) }
	p := langindex.New(root, limits, langOf, extractFile)
	persist := langindex.Persist[[]Function]{
		Saved: func(string) { x.persist(p) },
	}
	if seeded {
		persist.Load = func(string) map[string]langindex.Seeded[[]Function] {
			seed, corrupt := loadCache(x.cacheFile, root)
			if corrupt {
				x.mu.Lock()
				if x.project == p {
					x.corrupt = p
				}
				x.mu.Unlock()
			}
			return seed
		}
	}
	p.SetPersist(persist)
	return p
}

// persist writes walk p to the cache when p is still the index's walk — a
// walk a rebuild or a setting already replaced is stale. It runs on the scan
// goroutine; a failed write only costs the next session its warm start.
func (x *Index) persist(p *langindex.Index[[]Function]) {
	x.mu.Lock()
	ok := x.opts.Enabled && x.project == p
	x.mu.Unlock()
	if ok {
		_ = saveCache(x.cacheFile, x.root, p)
	}
}

// classify is the walk's classifier: indexLang for a file of a language with
// a spec that no exclude glob matches, "" for everything else.
func classify(root string, exclude []string, path string) string {
	l, ok := lang.ByPath(path)
	if !ok || l.Complexity == nil {
		return ""
	}
	if Excluded(root, exclude, path) {
		return ""
	}
	return indexLang
}

// Excluded reports whether one of the complexity.exclude globs matches
// path: against the project-relative slash path, and — for a pattern
// without a slash — against the base name too.
func Excluded(root string, exclude []string, path string) bool {
	if len(exclude) == 0 {
		return false
	}
	rel := path
	if root != "" {
		if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	rel = filepath.ToSlash(rel)
	base := filepath.Base(path)
	for _, pat := range exclude {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if pathglob.Match(pat, rel) {
			return true
		}
		if !strings.Contains(pat, "/") && pathglob.Match(pat, base) {
			return true
		}
	}
	return false
}

// extractFile is the walk's extractor: it measures the file as the language
// its path names. A file the engine cannot measure (no tree) contributes
// nothing and is not indexed.
func extractFile(path, _ string, text string, only func(string) bool) map[string][]Function {
	if !only(indexLang) {
		return nil
	}
	l, ok := lang.ByPath(path)
	if !ok || l.Complexity == nil {
		return nil
	}
	fns, err := Measure(l.ID, strings.Split(text, "\n"))
	if err != nil {
		return nil
	}
	if fns == nil {
		fns = []Function{}
	}
	return map[string][]Function{indexLang: fns}
}

// Available reports whether the build can parse at least one language with a
// spec (cgo + grammar); without it the index can never fill.
func (x *Index) Available() bool { return Available() }

// InvalidateFile re-measures path through the walk's worker — a buffer
// save, or an on-disk change the watcher reported; a removed file drops out.
// A path no spec language claims, or an excluded one, is ignored without
// queueing. A relative path resolves against the root.
func (x *Index) InvalidateFile(path string) {
	if path == "" {
		return
	}
	x.mu.Lock()
	p := x.project
	root, exclude := x.root, x.opts.Exclude
	x.mu.Unlock()
	if p == nil {
		return
	}
	if !filepath.IsAbs(path) && root != "" {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if classify(root, exclude, path) == "" {
		return
	}
	x.mu.Lock()
	x.armWatchLocked()
	x.mu.Unlock()
	p.Invalidate(path)
}

// ScanDone reports whether the walk finished (true for a disabled index or
// one not started yet: there is nothing to wait for).
func (x *Index) ScanDone() bool {
	x.mu.Lock()
	p, started := x.project, x.started
	x.mu.Unlock()
	return p == nil || !started || p.Done(indexLang)
}

// Snapshot returns the current content as an immutable View. It is cheap
// while nothing changed (the map is reused) and rebuilds the map from the
// walk once per content generation.
func (x *Index) Snapshot() View {
	x.mu.Lock()
	p := x.project
	if p == nil {
		v := View{Gen: x.gen, Files: map[string][]Function{}}
		x.mu.Unlock()
		return v
	}
	key := viewKey{projGen: p.Gen(), project: p}
	if x.view != nil && x.viewKey == key {
		v := *x.view
		v.Gen = x.gen
		x.mu.Unlock()
		return v
	}
	x.mu.Unlock()
	files := map[string][]Function{}
	p.Each([]string{indexLang}, func(path string, fns []Function) {
		files[path] = fns
	})
	x.mu.Lock()
	v := View{Gen: x.gen, Files: files}
	if x.project == p {
		x.view, x.viewKey = &v, key
	}
	x.mu.Unlock()
	return v
}

// Stats implements the status report (see Stats).
func (x *Index) Stats() Stats {
	x.mu.Lock()
	p := x.project
	enabled := x.opts.Enabled
	started := x.started
	corrupt := p != nil && x.corrupt == p
	x.mu.Unlock()
	s := Stats{Enabled: enabled, Unavailable: !x.Available()}
	if p == nil {
		return s
	}
	s.Scanning = started && !p.Done(indexLang)
	s.LastScan, s.Truncated = p.ScanInfo(indexLang)
	s.Cached = p.Cached(indexLang)
	s.CacheCorrupt = corrupt
	v := x.Snapshot()
	s.Files = len(v.Files)
	s.Functions = v.Functions()
	return s
}

// SetOnChange installs the callback the index fires whenever its content
// generation moved — the walk finished, a file was re-measured, the index
// was dropped or rebuilt. It runs off the UI goroutine, debounced by the
// settle window, never while the index holds its lock, with the Gen the
// next Snapshot carries. Pass nil to remove it.
func (x *Index) SetOnChange(fn func(gen int)) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.onChange = fn
	x.armWatchLocked()
}

// SetOnScan installs the callback fired once for every completed walk — the
// initial scan and every Rebuild — with the stats it ended on, for the
// complexity.scan telemetry op. Same goroutine and lock rules as
// SetOnChange. Pass nil to remove it.
func (x *Index) SetOnScan(fn func(Stats)) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.onScan = fn
	x.armWatchLocked()
}

// armChangeLocked (re-)starts the settle timer; the caller holds x.mu.
func (x *Index) armChangeLocked() {
	if x.onChange == nil && x.onScan == nil {
		return
	}
	if x.chTimer == nil {
		x.chTimer = time.AfterFunc(changeSettle, x.checkChange)
		return
	}
	x.chTimer.Reset(changeSettle)
}

// armWatchLocked arms the settle timer and keeps it polling for changeWatch,
// for the events whose work lands asynchronously. The caller holds x.mu.
func (x *Index) armWatchLocked() {
	x.watchUntil = time.Now().Add(changeWatch)
	x.armChangeLocked()
}

// checkChange runs on the settle timer: it bumps the generation and fires
// the callback when the walk's content moved since the last notification,
// and reports a finished walk once.
func (x *Index) checkChange() {
	x.mu.Lock()
	fn, scan, p := x.onChange, x.onScan, x.project
	if fn == nil && scan == nil {
		x.mu.Unlock()
		return
	}
	var key viewKey
	done := true
	if p != nil {
		key = viewKey{projGen: p.Gen(), project: p}
		done = !x.started || p.Done(indexLang)
	}
	changed := key != x.lastGen || x.pending
	x.lastGen, x.pending = key, false
	if changed {
		x.gen++
	}
	gen := x.gen
	report := scan != nil && p != nil && done && x.scanned != p
	if report {
		x.scanned = p
	}
	if changed || !done || time.Now().Before(x.watchUntil) {
		x.armChangeLocked()
	}
	x.mu.Unlock()
	if changed && fn != nil {
		fn(gen)
	}
	if report {
		scan(x.Stats())
	}
}

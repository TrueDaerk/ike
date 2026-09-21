// Package phpindex is the workspace-wide PHP declaration index (Epic 0520,
// #2667). Intelephense resolves `$this` inside a trait body as the trait
// itself, so every member that only exists on the trait's consumers is
// unknown to the server. This index knows the *edges* — which classes and
// enums use which traits, which traits use other traits, which class extends
// which — plus every class-like declaration's members, and answers the
// consumer-scope questions the trait features of the epic (completion,
// diagnostics, navigation, references, rename) ask.
//
// It is not a type inferencer: it holds declarations and edges, never
// expression types. `$foo->bar()` on an arbitrary variable stays with the
// server.
//
// Storage is the shared per-language project walk (internal/complete/
// langindex): one scan of every *.php file under the project root through a
// tree-sitter extractor (extract.go), re-extraction of a changed file
// through the walk's single worker, and open buffers overriding their on-disk
// file from the editor's change events (debounced off the Update goroutine).
// Queries (query.go) run on a snapshot derived once per content generation.
//
// Without cgo the highlight layer has no syntax tree: the index stays empty
// and Stats reports it unavailable; nothing panics.
package phpindex

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ike/internal/complete/langindex"
	"ike/internal/config"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/lang"
)

// Scan limits: PHP legacy classes run long, so the per-file cap sits above
// the symbol index's; the file cap is the php.index.max_files setting.
const (
	maxFileSize = 512 << 10
	// bufferDebounce is how long after the last keystroke an edited buffer
	// re-extracts; extraction is a full parse and never runs on the UI
	// goroutine.
	bufferDebounce = 150 * time.Millisecond
	// changeSettle paces the content-change callback (#2669): it sits above
	// bufferDebounce so the check that follows a keystroke already sees the
	// re-extraction, and it is the debounce that keeps one keystroke from
	// refiltering the world.
	changeSettle = 250 * time.Millisecond
	// changeWatch is how long the settle poll keeps looking after an event
	// whose work lands asynchronously (see armWatchLocked).
	changeWatch = 2 * time.Second
)

// Options are the [php] settings the index runs under (#2667). Enabled is
// the master switch (php.trait_index); the rest are the php.index.* keys.
type Options struct {
	Enabled       bool
	ParentDepth   int
	IncludeVendor bool
	MaxFiles      int
}

// FromConfig maps the typed [php] section to Options.
func FromConfig(c config.PHP) Options {
	return Options{
		Enabled:       c.TraitIndex,
		ParentDepth:   c.Index.ParentDepth,
		IncludeVendor: c.Index.IncludeVendor,
		MaxFiles:      c.Index.MaxFiles,
	}
}

// Stats describes the index for a status popup and telemetry (#2673).
type Stats struct {
	// Enabled mirrors php.trait_index; a disabled index holds nothing.
	Enabled bool
	// Unavailable is set when the build has no PHP syntax tree (no cgo /
	// no grammar): the index can never fill.
	Unavailable bool
	// Scanning is true while the initial project walk runs.
	Scanning bool
	// Files counts indexed files (disk plus open buffers), Declarations the
	// class-like declarations in them, Edges the trait-use and extends /
	// implements edges between declarations.
	Files        int
	Declarations int
	Edges        int
	// LastScan is the project walk's duration; Truncated says it stopped at
	// php.index.max_files before the tree was exhausted.
	LastScan  time.Duration
	Truncated bool
}

// bufDoc is one observed open buffer; its extraction overrides the on-disk
// file of the same path. gen counts Observe updates (the #2193 pattern):
// extraction snapshots it and only a matching gen on install clears dirty.
type bufDoc struct {
	text  string
	decls fileDecls
	ok    bool // decls is a valid extraction
	dirty bool
	gen   uint64
}

// Index is the PHP declaration index. It implements complete.EventObserver
// and complete.FileObserver and is registered on the completion engine as
// an observer (Engine.RegisterObserver); it is not a completion source —
// the trait completion source (#2668) queries it.
type Index struct {
	mu      sync.Mutex
	root    string
	opts    Options
	project *langindex.Index[fileDecls] // nil while disabled
	buffers map[string]*bufDoc
	bufGen  uint64
	timer   *time.Timer
	// snap is the query snapshot derived for snapKey; a changed key
	// rebuilds it lazily on the next query.
	snap    *snapshot
	snapKey snapKey

	// onChange is the debounced content-change callback (#2669) and
	// chTimer its settle timer; lastGen is the generation the last
	// notification was sent for, watchUntil the deadline the settle poll
	// keeps checking to for an asynchronous re-extraction. See SetOnChange.
	onChange   func()
	chTimer    *time.Timer
	lastGen    snapKey
	watchUntil time.Time

	// onScan is the per-scan telemetry callback (#2673) and scanned the
	// project walk whose completion was already reported through it, so a
	// finished scan is announced exactly once. See SetOnScan.
	onScan  func(Stats)
	scanned *langindex.Index[fileDecls]

	availOnce sync.Once
	avail     bool
}

// snapKey identifies the content a snapshot was built from.
type snapKey struct {
	projGen, bufGen uint64
	depth           int
	project         *langindex.Index[fileDecls]
}

// New returns the index over the project at root ("" indexes nothing) and,
// when opts.Enabled, starts the scan in the background.
func New(root string, opts Options) *Index {
	x := &Index{root: root, buffers: map[string]*bufDoc{}}
	x.Reconfigure(opts)
	return x
}

// Root is the project root the index scans.
func (x *Index) Root() string { return x.root }

// Options returns the settings the index currently runs under.
func (x *Index) Options() Options {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.opts
}

// Reconfigure applies changed [php] settings live: turning the master
// switch off drops the index (buffers included), turning it on builds and
// scans it, a changed vendor/file cap rescans, and a changed parent depth
// only refreshes the derived snapshot.
func (x *Index) Reconfigure(opts Options) {
	x.mu.Lock()
	defer x.mu.Unlock()
	prev := x.opts
	x.opts = opts
	switch {
	case !opts.Enabled:
		x.project = nil
		x.buffers = map[string]*bufDoc{}
		x.snap = nil
		if x.timer != nil {
			x.timer.Stop()
			x.timer = nil
		}
		if x.chTimer != nil {
			x.chTimer.Stop()
		}
	case x.project == nil || prev.IncludeVendor != opts.IncludeVendor || prev.MaxFiles != opts.MaxFiles:
		x.project = x.newProject(opts)
		x.project.Ensure("php")
		x.snap = nil
		x.armWatchLocked()
	default:
		x.snap = nil
		x.armChangeLocked()
	}
}

// Rebuild drops the project walk and starts a fresh one (#2673): the escape
// hatch for a workspace that changed underneath ike — a branch switch with
// thousands of files, a generator run — where no watcher event ever arrived.
// Observed open buffers survive it; they are the editor's live truth, not
// the walk's, and re-reading them would only lose the unsaved overrides.
//
// It reports false when there is nothing to rebuild — php.trait_index is off,
// or the build cannot parse PHP at all — so the command can say so instead of
// pretending a scan started.
func (x *Index) Rebuild() bool {
	if !x.Available() {
		return false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.opts.Enabled {
		return false
	}
	x.project = x.newProject(x.opts)
	x.project.Ensure("php")
	x.snap = nil
	x.armWatchLocked()
	return true
}

// newProject builds the walk for opts: PHP files only (a custom classifier
// keeps the walk from reading other languages' files for embedded PHP),
// vendor/ kept when asked.
func (x *Index) newProject(opts Options) *langindex.Index[fileDecls] {
	limits := langindex.Limits{MaxFileSize: maxFileSize, MaxFiles: opts.MaxFiles}
	if limits.MaxFiles <= 0 {
		limits.MaxFiles = 20000
	}
	if opts.IncludeVendor {
		limits.KeepDirs = []string{"vendor"}
	}
	return langindex.New(x.root, limits, phpOnly, extractFile)
}

// phpOnly classifies a path for the walk: "php" for a PHP file, "" for
// everything else — nothing else is read.
func phpOnly(path string) string {
	if l, ok := lang.ByPath(path); ok && l.ID == "php" {
		return "php"
	}
	return ""
}

// Available reports whether the build can parse PHP at all (cgo + grammar).
func (x *Index) Available() bool {
	x.availOnce.Do(func() {
		x.avail = highlight.SyntaxTree("php", []string{"<?php"}) != nil
	})
	return x.avail
}

// Observe implements complete.EventObserver: a PHP buffer's change event
// stashes the text and arms the debounce; extraction runs on the timer's
// goroutine, never here.
func (x *Index) Observe(ev host.EditorEvent) {
	if ev.Kind != host.EditorChange || ev.Path == "" || phpOnly(ev.LangName()) != "php" {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.project == nil {
		return
	}
	key := cleanPath(ev.Path)
	if ev.Large {
		if _, had := x.buffers[key]; had {
			delete(x.buffers, key)
			x.bufGen++
			x.armChangeLocked()
		}
		return
	}
	d := x.buffers[key]
	if d == nil {
		d = &bufDoc{}
		x.buffers[key] = d
	}
	d.text, d.dirty = ev.Text, true
	d.gen++
	if x.timer == nil {
		x.timer = time.AfterFunc(bufferDebounce, x.Flush)
	} else {
		x.timer.Reset(bufferDebounce)
	}
	x.armChangeLocked()
}

// Flush extracts every dirty buffer now, on the caller's goroutine (the
// debounce timer's, or a test's). Extraction runs outside the lock; a buffer
// edited meanwhile stays dirty for the next flush.
func (x *Index) Flush() {
	type job struct {
		d    *bufDoc
		key  string
		text string
		gen  uint64
	}
	x.mu.Lock()
	var jobs []job
	for key, d := range x.buffers {
		if d.dirty {
			jobs = append(jobs, job{d: d, key: key, text: d.text, gen: d.gen})
		}
	}
	x.mu.Unlock()
	for _, j := range jobs {
		decls, ok := extractText(j.key, j.text)
		x.mu.Lock()
		if x.buffers[j.key] == j.d {
			j.d.decls, j.d.ok = decls, ok
			if j.d.gen == j.gen {
				j.d.dirty = false
			}
			x.bufGen++
			x.armChangeLocked()
		}
		x.mu.Unlock()
	}
}

// InvalidateFile implements complete.FileObserver: an on-disk change of a
// PHP file re-extracts it through the walk's worker; a removed file drops
// out. Other files are ignored without queueing.
func (x *Index) InvalidateFile(path string) {
	if phpOnly(path) != "php" {
		return
	}
	x.mu.Lock()
	p := x.project
	x.armWatchLocked()
	x.mu.Unlock()
	if p != nil {
		p.Invalidate(path)
	}
}

// ScanDone reports whether the project walk finished (true for a disabled
// index: there is nothing to wait for).
func (x *Index) ScanDone() bool {
	x.mu.Lock()
	p := x.project
	x.mu.Unlock()
	return p == nil || p.Done("php")
}

// Stats implements the status report (see Stats).
func (x *Index) Stats() Stats {
	x.mu.Lock()
	p := x.project
	enabled := x.opts.Enabled
	x.mu.Unlock()
	s := Stats{Enabled: enabled, Unavailable: !x.Available()}
	if p == nil {
		return s
	}
	s.Scanning = !p.Done("php")
	s.LastScan, s.Truncated = p.ScanInfo("php")
	snap := x.snapshot()
	if snap != nil {
		s.Files = len(snap.files)
		s.Declarations = len(snap.decls)
		s.Edges = snap.edges
	}
	return s
}

// snapshot returns the query snapshot for the current content, rebuilding
// it when the project walk, a buffer or the parent depth changed.
func (x *Index) snapshot() *snapshot {
	x.mu.Lock()
	p := x.project
	if p == nil {
		x.mu.Unlock()
		return nil
	}
	key := snapKey{projGen: p.Gen(), bufGen: x.bufGen, depth: x.opts.ParentDepth, project: p}
	if x.snap != nil && x.snapKey == key {
		s := x.snap
		x.mu.Unlock()
		return s
	}
	// Collect under the lock (Each holds the walk's lock; the buffers are
	// ours), derive outside it.
	files := map[string]fileDecls{}
	p.Each([]string{"php"}, func(path string, v fileDecls) { files[path] = v })
	for path, d := range x.buffers {
		if d.ok {
			files[path] = d.decls
		}
	}
	x.mu.Unlock()
	s := buildSnapshot(files, key.depth)
	x.mu.Lock()
	if x.project == p {
		x.snap, x.snapKey = s, key
	}
	x.mu.Unlock()
	return s
}

// SetOnChange installs the callback the index fires whenever its content
// generation changed — the initial scan finished, a watched file was
// re-extracted, an observed buffer was re-parsed, or a setting moved the
// derived scope (#2669). It is called off the UI goroutine, debounced by
// changeSettle, and never while the index holds its lock, so the callback
// may query the index. Pass nil to remove it.
//
// The callback is armed by the events that *can* change the content and then
// verified against the generation counters, because the work they kick off is
// asynchronous: a re-extraction queues behind the walk's worker and the
// initial scan runs for as long as it runs. A check that still sees a running
// scan re-arms, so the settle poll is bounded by the scan and stops the
// moment the generation is stable.
func (x *Index) SetOnChange(fn func()) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.onChange = fn
	x.armWatchLocked()
}

// SetOnScan installs the callback fired once for every completed project
// walk (#2673) — the initial scan and every Rebuild — with the stats the walk
// ended on: duration, file count and the truncated flag the telemetry op
// php.trait.index_scan carries. Like the change callback it runs off the UI
// goroutine and never while the index holds its lock, so it may query the
// index. Pass nil to remove it.
//
// It shares the settle timer with SetOnChange: the poll that watches the
// content generation is already bounded by the scan, so completion is noticed
// without a second timer.
func (x *Index) SetOnScan(fn func(Stats)) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.onScan = fn
	x.armWatchLocked()
}

// armChangeLocked (re-)starts the settle timer; the caller holds x.mu.
func (x *Index) armChangeLocked() {
	if (x.onChange == nil && x.onScan == nil) || x.project == nil {
		return
	}
	if x.chTimer == nil {
		x.chTimer = time.AfterFunc(changeSettle, x.checkChange)
		return
	}
	x.chTimer.Reset(changeSettle)
}

// armWatchLocked arms the settle timer and keeps it checking for changeWatch,
// for the events whose work lands asynchronously (a watcher re-extraction
// queues behind the walk's worker, a rescan behind the walk itself) and would
// otherwise be missed by a single check. The caller holds x.mu.
func (x *Index) armWatchLocked() {
	x.watchUntil = time.Now().Add(changeWatch)
	x.armChangeLocked()
}

// checkChange runs on the settle timer: it fires the callback when the
// content generation moved since the last notification.
func (x *Index) checkChange() {
	x.mu.Lock()
	fn, scan, p := x.onChange, x.onScan, x.project
	if (fn == nil && scan == nil) || p == nil {
		x.mu.Unlock()
		return
	}
	gen := snapKey{projGen: p.Gen(), bufGen: x.bufGen, depth: x.opts.ParentDepth, project: p}
	changed := gen != x.lastGen
	x.lastGen = gen
	done := p.Done("php")
	// A finished walk is reported once, keyed on the walk itself: a rebuild
	// installs a new one and therefore earns its own scan event.
	report := scan != nil && done && x.scanned != p
	if report {
		x.scanned = p
	}
	// Keep settling while the scan is still producing generations or an
	// asynchronous re-extraction may still land; a change re-arms once so
	// the follow-up check can confirm the content settled.
	if changed || !done || time.Now().Before(x.watchUntil) {
		x.armChangeLocked()
	}
	x.mu.Unlock()
	if changed && fn != nil {
		fn()
	}
	if report {
		scan(x.Stats())
	}
}

// shortName is the last segment of a (possibly qualified) PHP name.
func shortName(fqn string) string {
	if i := strings.LastIndex(fqn, "\\"); i >= 0 {
		return fqn[i+1:]
	}
	return fqn
}

// cleanPath normalizes a path the way the walk records it.
func cleanPath(path string) string {
	return filepath.Clean(path)
}

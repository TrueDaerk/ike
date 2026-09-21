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
	case x.project == nil || prev.IncludeVendor != opts.IncludeVendor || prev.MaxFiles != opts.MaxFiles:
		x.project = x.newProject(opts)
		x.project.Ensure("php")
		x.snap = nil
	default:
		x.snap = nil
	}
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

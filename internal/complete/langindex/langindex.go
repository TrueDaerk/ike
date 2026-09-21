// Package langindex is the lazy, per-language project index the word and
// symbol completion sources share (#2652). The sources used to walk the
// project once at construction and pool every file of every type; now a
// buffer is offered only code tokens of its own language, so the walk runs
// per language, the first time a buffer of that language asks — a Python
// buffer scans *.py plus the python fragments embedded in other files, and a
// Go buffer opened later triggers the Go scan. Nothing is scanned for a
// language nobody completes in, and the language filter at query time is a
// map lookup.
//
// What a file contributes is the source's business (the word index
// tokenizes code segments, the symbol index reads grammar captures); the
// index owns the walk, its caps and skips, the per-language bookkeeping,
// and the single-worker watcher re-extraction (#2176).
package langindex

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// skipDirs are directory names the walk never descends into.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "__pycache__": true, ".git": true, ".venv": true, "venv": true,
}

// Extractor turns one file into its per-language contributions: the map is
// keyed by language id and holds only the languages only accepts. text is
// the file's content; host is its own language id ("" when no language
// claims the path). It runs off every lock, on a scan or worker goroutine.
type Extractor[T any] func(path, host, text string, only func(langID string) bool) map[string]T

// Limits bound one language's walk: the per-file byte cap and how many files
// the walk may read before it stops. KeepDirs names directories of the
// built-in skip list the walk descends into anyway (#2667: the PHP
// declaration index opts into vendor/ through php.index.include_vendor).
type Limits struct {
	MaxFileSize int64
	MaxFiles    int
	KeepDirs    []string
}

// entry is one language's share of the index. dur and truncated describe
// the scan (#2667, Stats): how long the walk took and whether it stopped at
// MaxFiles before the tree was exhausted.
type entry[T any] struct {
	files     map[string]T
	done      bool
	dur       time.Duration
	truncated bool
}

// Index is the per-language project index. Zero value is not usable; use New.
type Index[T any] struct {
	mu     sync.Mutex
	root   string
	limits Limits
	langOf func(path string) string
	ext    Extractor[T]
	langs  map[string]*entry[T]
	// embeds caches, per file read by some scan, the languages of the
	// fragments embedded in it, so the next language's scan skips the files
	// that cannot contain it without parsing them again. A watcher
	// invalidation drops the entry.
	embeds map[string][]string
	// gen counts content changes (#2667): a finished scan and every
	// re-extraction bump it, so a reader caching a derived view (the PHP
	// index's edge tables) knows when to rebuild without diffing files.
	gen uint64

	// Invalidation queue (#2176): watcher events re-extract through one
	// worker goroutine instead of one goroutine per event.
	invalMu   sync.Mutex
	invalQ    []string
	invalSet  map[string]bool
	invalBusy bool
}

// New returns an index over root ("" indexes nothing: every language reads
// as scanned and empty). langOf classifies a path; nil means lang.ByPath.
func New[T any](root string, limits Limits, langOf func(string) string, ext Extractor[T]) *Index[T] {
	if langOf == nil {
		langOf = LangOf
	}
	return &Index[T]{
		root: root, limits: limits, langOf: langOf, ext: ext,
		langs:  map[string]*entry[T]{},
		embeds: map[string][]string{},
	}
}

// LangOf is the default path classifier: the registered language's id, or
// "" when no language claims the path.
func LangOf(path string) string {
	if l, ok := lang.ByPath(path); ok {
		return l.ID
	}
	return ""
}

// Ensure starts the scan for every listed language that has none yet, in
// the background, and returns at once. Safe to call per query.
func (x *Index[T]) Ensure(langs ...string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, id := range langs {
		if id == "" || x.langs[id] != nil {
			continue
		}
		e := &entry[T]{files: map[string]T{}}
		x.langs[id] = e
		if x.root == "" {
			e.done = true
			continue
		}
		go x.scan(id)
	}
}

// Done reports whether id's scan finished (or was never needed). A language
// nobody asked for is not done: its scan has not even started.
func (x *Index[T]) Done(id string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	e := x.langs[id]
	return e != nil && e.done
}

// Gen is the content generation (#2667): it changes whenever a scan
// finishes or a file is re-extracted, and only then.
func (x *Index[T]) Gen() uint64 {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.gen
}

// ScanInfo reports id's finished scan (#2667): the walk's duration and
// whether it stopped at Limits.MaxFiles. Zero values while the scan runs
// or when the language was never asked for.
func (x *Index[T]) ScanInfo(id string) (dur time.Duration, truncated bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	e := x.langs[id]
	if e == nil || !e.done {
		return 0, false
	}
	return e.dur, e.truncated
}

// Scanned lists the languages a scan was started for, sorted.
func (x *Index[T]) Scanned() []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	out := make([]string, 0, len(x.langs))
	for id := range x.langs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Each calls fn for every indexed file of the listed languages, under the
// index lock — fn must only read. A language without a scan contributes
// nothing (call Ensure first).
func (x *Index[T]) Each(langs []string, fn func(path string, v T)) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, id := range langs {
		e := x.langs[id]
		if e == nil {
			continue
		}
		for p, v := range e.files {
			fn(p, v)
		}
	}
}

// scan walks root for language id: files of the language itself, plus the
// files that embed fragments of it. Every file read records its embedded
// languages, so later scans consult the cache instead of the grammar.
func (x *Index[T]) scan(id string) {
	only := func(l string) bool { return l == id }
	files := map[string]T{}
	read := 0
	truncated := false
	started := time.Now()
	_ = filepath.WalkDir(x.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != x.root && (skipDirs[name] || strings.HasPrefix(name, ".")) && !contains(x.limits.KeepDirs, name) {
				return filepath.SkipDir
			}
			return nil
		}
		if read >= x.limits.MaxFiles {
			truncated = true
			return filepath.SkipAll
		}
		host := x.langOf(path)
		if host != id {
			x.mu.Lock()
			langs, known := x.embeds[path]
			x.mu.Unlock()
			if known && !contains(langs, id) {
				return nil
			}
			if !known && !mayEmbed(host) {
				return nil
			}
		}
		text, ok := x.readFile(path)
		if !ok {
			return nil
		}
		read++
		embeds := highlight.EmbeddedLangs(host, strings.Split(text, "\n"))
		x.mu.Lock()
		x.embeds[path] = embeds
		x.mu.Unlock()
		if host != id && !contains(embeds, id) {
			return nil
		}
		if v, ok := x.ext(path, host, text, only)[id]; ok {
			files[path] = v
		}
		return nil
	})
	x.mu.Lock()
	e := x.langs[id]
	for p, v := range files {
		e.files[p] = v
	}
	e.done = true
	e.dur = time.Since(started)
	e.truncated = truncated
	x.gen++
	x.mu.Unlock()
}

// mayEmbed reports whether a file of language host can carry fragments of
// another language at all: it needs a grammar (for its injection query) or
// a region detector. A grammar-less language embeds nothing, and a path no
// language claims is not read.
func mayEmbed(host string) bool {
	l, ok := lang.ByID(host)
	return ok && (l.Grammar != nil || l.Regions != nil)
}

// readFile reads path within the size cap, skipping directories and
// binaries (NUL in the head).
func (x *Index[T]) readFile(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > x.limits.MaxFileSize {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return "", false
	}
	return string(data), true
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Invalidate re-extracts path for every scanned language, off the caller's
// goroutine: paths queue behind one worker (#2176) — bounded disk
// concurrency — and a path already queued is not queued twice.
func (x *Index[T]) Invalidate(path string) {
	x.invalMu.Lock()
	if x.invalSet == nil {
		x.invalSet = map[string]bool{}
	}
	if !x.invalSet[path] {
		x.invalSet[path] = true
		x.invalQ = append(x.invalQ, path)
	}
	if !x.invalBusy {
		x.invalBusy = true
		go x.drain()
	}
	x.invalMu.Unlock()
}

// drain is the single re-extraction worker: it pops the queue until empty
// and exits; the next Invalidate restarts it.
func (x *Index[T]) drain() {
	for {
		x.invalMu.Lock()
		if len(x.invalQ) == 0 {
			x.invalBusy = false
			x.invalMu.Unlock()
			return
		}
		path := x.invalQ[0]
		x.invalQ = x.invalQ[1:]
		delete(x.invalSet, path)
		x.invalMu.Unlock()
		x.reextract(path)
	}
}

// reextract refreshes path's contribution to every scanned language.
func (x *Index[T]) reextract(path string) {
	scanned := x.Scanned()
	if len(scanned) == 0 {
		return
	}
	only := func(l string) bool { return contains(scanned, l) }
	host := x.langOf(path)
	var res map[string]T
	var embeds []string
	if text, ok := x.readFile(path); ok {
		lines := strings.Split(text, "\n")
		embeds = highlight.EmbeddedLangs(host, lines)
		res = x.ext(path, host, text, only)
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if res == nil {
		delete(x.embeds, path)
	} else {
		x.embeds[path] = embeds
	}
	for id, e := range x.langs {
		if v, ok := res[id]; ok {
			e.files[path] = v
		} else {
			delete(e.files, path)
		}
	}
	x.gen++
}

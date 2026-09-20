// Package symbols is the symbol-index completion source (Roadmap 0410, #853):
// project-wide identifiers extracted through the tree-sitter highlight layer
// (functions, methods, types, constants, constructors — the captures the
// language grammars already produce), with no server round-trip. CSS files
// contribute their class names and IDs, offered inside HTML `class=`/`id=`
// attribute values — the cross-file case language servers are structurally
// weak at.
//
// Symbols are scoped by language (#2652): a buffer is offered declarations of
// its own language family (complete.Request.Langs) only — a Go function is
// not a Python candidate — and an embedded fragment's declarations belong to
// the fragment's language, so a ```go fence in Markdown feeds the Go index.
//
// Freshness: open buffers re-extract lazily from the engine's forwarded
// change events; on-disk changes invalidate through the watcher
// (Engine.NotifyFileChanged). The project index scans one language at a
// time, on the first request from a buffer of that language.
package symbols

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"ike/internal/complete"
	"ike/internal/complete/langindex"
	"ike/internal/config"
	"ike/internal/fuzzy"
	"ike/internal/highlight"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// Scan limits — parsing is per-file tree-sitter work, so the caps sit lower
// than the word index's.
const (
	maxFileSize  = 128 << 10
	maxScanFiles = 2000
	maxResults   = 200
)

// captureKinds maps the grammar capture names worth indexing to completion
// item kinds. Builtins and call-sites are noise, not symbols.
var captureKinds = map[string]int{
	"function":        protocol.KindFunction,
	"function.method": protocol.KindMethod,
	"constructor":     protocol.KindConstructor,
	"type":            protocol.KindClass,
	"constant":        protocol.KindConstant,
}

var (
	cssClassRe = regexp.MustCompile(`\.([A-Za-z_][A-Za-z0-9_-]*)`)
	cssIDRe    = regexp.MustCompile(`#([A-Za-z_][A-Za-z0-9_-]*)`)
	// attrRe matches an unclosed class=/id= attribute value at end of the
	// head; the leading whitespace requirement keeps data-class & co. out.
	attrRe = regexp.MustCompile(`(?i)\s(class|id)\s*=\s*("[^"]*|'[^']*)$`)
)

// sym is one indexed symbol.
type sym struct {
	name string
	kind int
}

// fileIndex is one language's contribution from one file.
type fileIndex struct {
	syms    []sym
	classes map[string]struct{}
	ids     map[string]struct{}
}

// doc is one observed open buffer; its extraction overrides the on-disk index
// for the same path. lang is the name extraction resolves the buffer's
// grammar by — the file path, or the synthetic name of a file-less buffer's
// chosen language (#2048), which is why it is stored instead of re-derived
// from the map key.
// gen counts Observe updates (#2193): the query snapshots it before extracting
// outside the lock, and only a matching gen on re-install may clear dirty.
type doc struct {
	text  string
	lang  string
	idx   map[string]fileIndex // per language id
	dirty bool
	gen   uint64
}

// Source is the symbol index. It implements complete.Source,
// complete.EventObserver and complete.FileObserver.
type Source struct {
	mu      sync.RWMutex
	buffers map[string]*doc
	project *langindex.Index[fileIndex]
}

// New returns the source over the project at root ("" indexes no files).
// Nothing is scanned until a buffer of some language asks.
func New(root string) *Source {
	return &Source{
		buffers: map[string]*doc{},
		project: langindex.New(root, langindex.Limits{MaxFileSize: maxFileSize, MaxFiles: maxScanFiles}, langOf, extractFile),
	}
}

// Name implements complete.Source.
func (s *Source) Name() string { return "symbols" }

// Priority implements complete.Source: below the server, above the word index.
func (s *Source) Priority() int { return ilsp.PrioritySymbols }

// Observe implements complete.EventObserver: buffer changes stash the text
// and re-extract lazily on the next query.
func (s *Source) Observe(ev host.EditorEvent) {
	if ev.Kind != host.EditorChange {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.Large {
		delete(s.buffers, ev.BufKey())
		return
	}
	d := s.buffers[ev.BufKey()]
	if d == nil {
		d = &doc{}
		s.buffers[ev.BufKey()] = d
	}
	if d.lang != ev.LangName() {
		// "Treat Buffer as …" switched the language under the buffer
		// (#2048): the stashed index belongs to the old grammar.
		d.lang, d.idx = ev.LangName(), nil
	}
	d.text, d.dirty = ev.Text, true
	d.gen++
}

// InvalidateFile implements complete.FileObserver: an on-disk change
// re-extracts the file for every scanned language off the caller's
// goroutine, queued behind the index's single worker (#2176).
func (s *Source) InvalidateFile(path string) { s.project.Invalidate(path) }

// Complete implements complete.Source. Inside an HTML class=/id= attribute it
// offers the project's CSS class names / IDs; elsewhere the indexed symbols
// of the request's language family, current file first.
func (s *Source) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	// Extraction (tree-sitter over full texts) must not run under the lock
	// (#2193): Observe is reached synchronously from the UI's Update and would
	// block behind it. Snapshot under the lock, extract unlocked, re-install —
	// a doc edited mid-extraction stays dirty (gen moved), and a doc whose
	// language switched (#2048) discards the wrong-grammar result entirely.
	type extraction struct {
		d          *doc
		lang, text string
		gen        uint64
	}
	s.mu.Lock()
	cur := s.buffers[req.BufKey()]
	curText := ""
	if cur != nil {
		curText = cur.text
	}
	var jobs []extraction
	for _, d := range s.buffers {
		if d.dirty {
			jobs = append(jobs, extraction{d: d, lang: d.lang, text: d.text, gen: d.gen})
		}
	}
	s.mu.Unlock()

	line := lineAt(curText, req.Line)
	for i := range jobs {
		idx := extractText(jobs[i].lang, jobs[i].text, nil)
		s.mu.Lock()
		if jobs[i].d.lang == jobs[i].lang {
			jobs[i].d.idx = idx
			if jobs[i].d.gen == jobs[i].gen {
				jobs[i].d.dirty = false
			}
		}
		s.mu.Unlock()
	}

	langs := req.Langs()
	html := req.LangID() == "html" || isHTML(req.LangName())
	if html {
		s.project.Ensure(cssLangs()...)
	}
	s.project.Ensure(langs...)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if html {
		if attr, ok := htmlAttrContext(line, req.Col); ok {
			return s.cssItems(attr, cssPrefix(line, req.Col)), nil
		}
	}
	return s.symbolItems(req.BufKey(), identifierPrefix(line, req.Col), langs), nil
}

// symbolItems collects hump-matched symbols of langs, current file tiered
// first.
func (s *Source) symbolItems(curPath, prefix string, langs []string) []ilsp.CompletionItem {
	mode := completionCase()
	seen := map[string]bool{}
	var items []ilsp.CompletionItem
	add := func(fis []fileIndex, tier int) {
		var ss []sym
		for _, fi := range fis {
			for _, y := range fi.syms {
				if seen[y.name] || y.name == prefix || !matchesPrefix(y.name, prefix, mode) {
					continue
				}
				seen[y.name] = true
				ss = append(ss, y)
			}
		}
		sort.Slice(ss, func(i, j int) bool { return ss[i].name < ss[j].name })
		for _, y := range ss {
			if len(items) >= maxResults {
				return
			}
			items = append(items, ilsp.CompletionItem{
				Label:        y.name,
				InsertText:   y.name,
				Kind:         y.kind,
				SortText:     strconv.Itoa(tier) + strings.ToLower(y.name),
				LocalityTier: tier,
			})
		}
	}
	pick := func(d *doc) []fileIndex {
		var fis []fileIndex
		for _, l := range langs {
			if fi, ok := d.idx[l]; ok {
				fis = append(fis, fi)
			}
		}
		return fis
	}
	if d := s.buffers[curPath]; d != nil {
		add(pick(d), 0)
	}
	var others []fileIndex
	for path, d := range s.buffers {
		if path != curPath {
			others = append(others, pick(d)...)
		}
	}
	add(others, 1)
	var project []fileIndex
	s.project.Each(langs, func(path string, fi fileIndex) {
		if s.buffers[path] == nil {
			project = append(project, fi)
		}
	})
	add(project, 2)
	return items
}

// cssItems collects the project's class names or IDs for an HTML attribute.
func (s *Source) cssItems(attr, prefix string) []ilsp.CompletionItem {
	mode := completionCase()
	langs := cssLangs()
	seen := map[string]bool{}
	names := []string{}
	collect := func(fi fileIndex) {
		set := fi.classes
		if attr == "id" {
			set = fi.ids
		}
		for n := range set {
			if !seen[n] && n != prefix && matchesPrefix(n, prefix, mode) {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	for _, d := range s.buffers {
		for _, l := range langs {
			if fi, ok := d.idx[l]; ok {
				collect(fi)
			}
		}
	}
	s.project.Each(langs, func(path string, fi fileIndex) {
		if s.buffers[path] == nil {
			collect(fi)
		}
	})
	sort.Strings(names)
	if len(names) > maxResults {
		names = names[:maxResults]
	}
	items := make([]ilsp.CompletionItem, len(names))
	for i, n := range names {
		items[i] = ilsp.CompletionItem{
			Label:      n,
			InsertText: n,
			Kind:       protocol.KindValue,
			SortText:   "0" + strings.ToLower(n),
		}
	}
	return items
}

// --- extraction ---

// langOf classifies a path for the index: the registered language's id, or
// "css" for a stylesheet no plugin claims (the selector regex needs no
// grammar, so CSS classes reach HTML even in a build without the web
// plugin), else "".
func langOf(path string) string {
	if id := langindex.LangOf(path); id != "" {
		return id
	}
	if isCSS(path) {
		return "css"
	}
	return ""
}

// cssLangs lists the language ids stylesheets index under — the registered
// language of each stylesheet extension, "css" where none is — so the HTML
// attribute query reads the right scopes.
func cssLangs() []string {
	seen := map[string]bool{}
	var out []string
	for _, ext := range []string{"css", "scss", "less"} {
		id := langOf("x." + ext)
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// extractFile is the project index's extractor.
func extractFile(path, _, text string, only func(string) bool) map[string]fileIndex {
	return extractText(path, text, only)
}

// extractText extracts one text's symbols per language: grammar-backed
// segments (the host's own code and every embedded fragment, each under its
// language) through the highlight layer's captures, stylesheets by selector
// regex. Without cgo the highlight layer answers nothing and only CSS
// survives — the word index still covers those buffers.
func extractText(path, text string, only func(string) bool) map[string]fileIndex {
	out := map[string]fileIndex{}
	host := langOf(path)
	lines := strings.Split(text, "\n")
	css := cssLangs()
	for _, seg := range highlight.Segments(host, lines, only) {
		fi := out[seg.Lang]
		if contains(css, seg.Lang) {
			code := seg.CodeText(lines)
			fi.classes = matchSet(cssClassRe, code, fi.classes)
			fi.ids = matchSet(cssIDRe, code, fi.ids)
		} else {
			fi.syms = captureSyms(seg, lines, fi.syms)
		}
		out[seg.Lang] = fi
	}
	if isCSS(path) && (only == nil || only(host)) {
		if _, ok := out[host]; !ok {
			// No grammar for the stylesheet (no cgo, no plugin): the
			// regex runs over the raw text.
			out[host] = fileIndex{
				classes: matchSet(cssClassRe, text, nil),
				ids:     matchSet(cssIDRe, text, nil),
			}
		}
	}
	return out
}

// captureSyms appends the indexable captures of one segment to syms.
func captureSyms(seg highlight.Segment, lines []string, syms []sym) []sym {
	seen := map[string]bool{}
	for _, y := range syms {
		seen[y.name] = true
	}
	for _, sp := range seg.Spans {
		kind, ok := captureKinds[sp.Capture]
		if !ok || sp.Line < 0 || sp.Line >= len(lines) {
			continue
		}
		runes := []rune(lines[sp.Line])
		if sp.StartCol < 0 || sp.EndCol > len(runes) || sp.StartCol >= sp.EndCol {
			continue
		}
		name := string(runes[sp.StartCol:sp.EndCol])
		if len(name) < 2 || seen[name] || !isIdentifier(name) {
			continue
		}
		seen[name] = true
		syms = append(syms, sym{name: name, kind: kind})
	}
	return syms
}

func matchSet(re *regexp.Regexp, text string, out map[string]struct{}) map[string]struct{} {
	if out == nil {
		out = map[string]struct{}{}
	}
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out[m[1]] = struct{}{}
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// --- context helpers ---

// htmlAttrContext reports whether col on line sits inside an unclosed
// class="…" / id='…' attribute value, and which attribute.
func htmlAttrContext(line string, col int) (string, bool) {
	runes := []rune(line)
	if col > len(runes) {
		col = len(runes)
	}
	head := string(runes[:col])
	m := attrRe.FindStringSubmatch(head)
	if m == nil {
		return "", false
	}
	return strings.ToLower(m[1]), true
}

// cssPrefix is the partial class/ID name ending at col (CSS names include -).
func cssPrefix(line string, col int) string {
	return prefixBy(line, col, func(r rune) bool {
		return r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
	})
}

// identifierPrefix is the partial identifier ending at col.
func identifierPrefix(line string, col int) string {
	return prefixBy(line, col, func(r rune) bool {
		return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
	})
}

func prefixBy(line string, col int, ok func(rune) bool) string {
	runes := []rune(line)
	if col > len(runes) {
		col = len(runes)
	}
	start := col
	for start > 0 && ok(runes[start-1]) {
		start--
	}
	return string(runes[start:col])
}

func lineAt(text string, line int) string {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	return lines[line]
}

// matchesPrefix is the source-side pre-filter (#2650): the same JetBrains-style
// hump match the popup applies, under completion.case_sensitivity, so "gur"
// reaches GotoURLResolver from the local symbol index. An empty prefix passes
// everything up to the cap.
func matchesPrefix(w, prefix string, mode fuzzy.Case) bool {
	if prefix == "" {
		return true
	}
	_, ok := fuzzy.MatchHumpsCase(prefix, w, mode)
	return ok
}

// completionCase reads completion.case_sensitivity live, once per query.
func completionCase() fuzzy.Case {
	return fuzzy.ParseCase(config.Get().Completion.CaseSensitivity)
}

func isIdentifier(s string) bool {
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r)) {
			continue
		}
		return false
	}
	return true
}

func isCSS(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".css", ".scss", ".less":
		return true
	}
	return false
}

func isHTML(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm", ".xhtml":
		return true
	}
	return false
}

// ScanDone reports whether the project scans of the listed languages
// finished — with no argument, every scan started so far (tests).
func (s *Source) ScanDone(langs ...string) bool {
	if len(langs) == 0 {
		langs = s.project.Scanned()
	}
	for _, l := range langs {
		if !s.project.Done(l) {
			return false
		}
	}
	return true
}

// ScannedLangs lists the languages a project scan was started for (tests).
func (s *Source) ScannedLangs() []string { return s.project.Scanned() }

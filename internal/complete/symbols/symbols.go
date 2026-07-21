// Package symbols is the symbol-index completion source (Roadmap 0410, #853):
// project-wide identifiers extracted through the tree-sitter highlight layer
// (functions, methods, types, constants, constructors — the captures the
// language grammars already produce), with no server round-trip. CSS files
// contribute their class names and IDs, offered inside HTML `class=`/`id=`
// attribute values — the cross-file case language servers are structurally
// weak at.
//
// Freshness: open buffers re-extract lazily from the engine's forwarded
// change events; on-disk changes invalidate through the watcher
// (Engine.NotifyFileChanged). The one-shot background scan seeds the index.
package symbols

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"ike/internal/complete"
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

var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "__pycache__": true, ".git": true, ".venv": true, "venv": true,
}

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

// fileIndex is one file's contribution.
type fileIndex struct {
	syms    []sym
	classes map[string]struct{}
	ids     map[string]struct{}
}

// doc is one observed open buffer; its extraction overrides the on-disk scan
// for the same path.
type doc struct {
	text  string
	idx   fileIndex
	dirty bool
}

// Source is the symbol index. It implements complete.Source,
// complete.EventObserver and complete.FileObserver.
type Source struct {
	mu      sync.RWMutex
	buffers map[string]*doc
	files   map[string]fileIndex
	scanned bool
}

// New returns the source and starts the one-shot background scan under root
// ("" skips it).
func New(root string) *Source {
	s := &Source{buffers: map[string]*doc{}, files: map[string]fileIndex{}}
	if root == "" {
		s.scanned = true
		return s
	}
	go s.scan(root)
	return s
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
		delete(s.buffers, ev.Path)
		return
	}
	d := s.buffers[ev.Path]
	if d == nil {
		d = &doc{}
		s.buffers[ev.Path] = d
	}
	d.text, d.dirty = ev.Text, true
}

// InvalidateFile implements complete.FileObserver: an on-disk change
// re-extracts the file off the caller's goroutine (the buffer entry, when the
// file is open, keeps overriding it anyway).
func (s *Source) InvalidateFile(path string) {
	if !eligible(path) {
		return
	}
	go func() {
		idx, ok := extractDisk(path)
		s.mu.Lock()
		if ok {
			s.files[path] = idx
		} else {
			delete(s.files, path)
		}
		s.mu.Unlock()
	}()
}

// Complete implements complete.Source. Inside an HTML class=/id= attribute it
// offers the project's CSS class names / IDs; elsewhere the indexed symbols,
// current file first.
func (s *Source) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	s.mu.Lock()
	cur := s.buffers[req.Path]
	line := ""
	if cur != nil {
		cur.extract(req.Path)
		line = lineAt(cur.text, req.Line)
	}
	for path, d := range s.buffers {
		d.extract(path)
	}
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()
	if isHTML(req.Path) {
		if attr, ok := htmlAttrContext(line, req.Col); ok {
			return s.cssItems(attr, cssPrefix(line, req.Col)), nil
		}
	}
	return s.symbolItems(req.Path, identifierPrefix(line, req.Col)), nil
}

// symbolItems collects prefix-matched symbols, current file tiered first.
func (s *Source) symbolItems(curPath, prefix string) []ilsp.CompletionItem {
	seen := map[string]bool{}
	var items []ilsp.CompletionItem
	add := func(fi fileIndex, tier string) {
		var ss []sym
		for _, y := range fi.syms {
			if seen[y.name] || y.name == prefix || !matchesPrefix(y.name, prefix) {
				continue
			}
			ss = append(ss, y)
		}
		sort.Slice(ss, func(i, j int) bool { return ss[i].name < ss[j].name })
		for _, y := range ss {
			if len(items) >= maxResults {
				return
			}
			seen[y.name] = true
			items = append(items, ilsp.CompletionItem{
				Label:      y.name,
				InsertText: y.name,
				Kind:       y.kind,
				SortText:   tier + strings.ToLower(y.name),
			})
		}
	}
	if d := s.buffers[curPath]; d != nil {
		add(d.idx, "0")
	}
	for path, d := range s.buffers {
		if path != curPath {
			add(d.idx, "1")
		}
	}
	for path, fi := range s.files {
		if s.buffers[path] == nil {
			add(fi, "1")
		}
	}
	return items
}

// cssItems collects the project's class names or IDs for an HTML attribute.
func (s *Source) cssItems(attr, prefix string) []ilsp.CompletionItem {
	seen := map[string]bool{}
	names := []string{}
	collect := func(fi fileIndex) {
		set := fi.classes
		if attr == "id" {
			set = fi.ids
		}
		for n := range set {
			if !seen[n] && n != prefix && matchesPrefix(n, prefix) {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	for _, d := range s.buffers {
		collect(d.idx)
	}
	for path, fi := range s.files {
		if s.buffers[path] == nil {
			collect(fi)
		}
	}
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

// extract refreshes a dirty buffer.
func (d *doc) extract(path string) {
	if !d.dirty {
		return
	}
	d.idx = extractText(path, d.text)
	d.dirty = false
}

// extractDisk reads and extracts one file within the size cap.
func extractDisk(path string) (fileIndex, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxFileSize {
		return fileIndex{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileIndex{}, false
	}
	return extractText(path, string(data)), true
}

// extractText extracts one file's symbols: CSS files by selector regex,
// grammar-backed files through the highlight layer's captures. Without cgo
// the highlight layer answers nothing and only CSS survives — the word index
// still covers those projects.
func extractText(path, text string) fileIndex {
	if isCSS(path) {
		return fileIndex{
			classes: matchSet(cssClassRe, text),
			ids:     matchSet(cssIDRe, text),
		}
	}
	lines := strings.Split(text, "\n")
	spans := highlight.Highlight(path, lines)
	seen := map[string]bool{}
	var syms []sym
	for _, sp := range spans {
		kind, ok := captureKinds[sp.Capture]
		if !ok || sp.Line >= len(lines) {
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
	return fileIndex{syms: syms}
}

func matchSet(re *regexp.Regexp, text string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out[m[1]] = struct{}{}
	}
	return out
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

func matchesPrefix(w, prefix string) bool {
	if prefix == "" {
		return true
	}
	if len(w) < len(prefix) {
		return false
	}
	return strings.EqualFold(w[:len(prefix)], prefix)
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

// eligible reports whether a path can contribute to the index at all.
func eligible(path string) bool {
	return isCSS(path) || highlight.Supported(path)
}

// scan walks root once, extracting every eligible file within the caps.
func (s *Source) scan(root string) {
	files := map[string]fileIndex{}
	count := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if count >= maxScanFiles {
			return filepath.SkipAll
		}
		if !eligible(path) {
			return nil
		}
		if idx, ok := extractDisk(path); ok {
			files[path] = idx
			count++
		}
		return nil
	})
	s.mu.Lock()
	for p, fi := range files {
		s.files[p] = fi
	}
	s.scanned = true
	s.mu.Unlock()
}

// ScanDone reports whether the one-shot scan finished (tests).
func (s *Source) ScanDone() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scanned
}

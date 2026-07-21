// Package words is the word-index completion source (Roadmap 0410, #852):
// vim-keyword-level completion from identifier words seen in open buffers and
// a one-shot background scan of the project tree. It is instant, needs no
// server round-trip, and rescues the popup when a language server is slow,
// missing, or dead.
//
// Freshness: open buffers update incrementally — the engine forwards every
// EditorChange event (full text) and the word set re-extracts lazily on the
// next query. The project scan runs once at construction; edits to files not
// open in a buffer are not re-scanned (the buffer index covers everything the
// user actually types in).
package words

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"ike/internal/complete"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// Scan limits: the index is a convenience, not a database — bound the work.
const (
	maxFileSize  = 256 << 10 // per-file byte cap for the project scan
	maxScanFiles = 10000     // project-scan file cap
	maxResults   = 200       // per-query item cap (the editor fuzzy-filters further)
	minWordLen   = 3         // shorter identifiers are noise
)

// skipDirs are directory names the project scan never descends into.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, "__pycache__": true, ".git": true, ".venv": true, "venv": true,
}

// Source is the word index. It implements complete.Source and
// complete.EventObserver.
type Source struct {
	mu      sync.RWMutex
	buffers map[string]*buffer // open-buffer text + lazily extracted words
	project map[string]struct{}
	scanned bool // project scan finished (tests wait on it)
}

// buffer is one observed open buffer.
type buffer struct {
	text  string
	words map[string]struct{}
	dirty bool
}

// New returns the source and starts the one-shot project scan under root in
// the background ("" skips the scan).
func New(root string) *Source {
	s := &Source{buffers: map[string]*buffer{}, project: map[string]struct{}{}}
	if root == "" {
		s.scanned = true
		return s
	}
	go s.scan(root)
	return s
}

// Name implements complete.Source.
func (s *Source) Name() string { return "words" }

// Priority implements complete.Source: the word index loses every de-dup
// against the server and the symbol index.
func (s *Source) Priority() int { return ilsp.PriorityWords }

// Observe implements complete.EventObserver: change events stash the buffer's
// latest text; extraction happens lazily on the next query, off this (UI)
// goroutine. Large-file changes carry no text and drop the buffer's index.
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
	b := s.buffers[ev.Path]
	if b == nil {
		b = &buffer{}
		s.buffers[ev.Path] = b
	}
	b.text, b.dirty = ev.Text, true
}

// Complete implements complete.Source: candidates are identifier words
// case-insensitively prefixed by the partial word at the request position —
// current buffer first, then other buffers, then the project scan — capped at
// maxResults. The word being typed itself is excluded. SortText encodes the
// locality tier, so the merged popup lists nearer words first.
func (s *Source) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	s.mu.Lock()
	cur := s.buffers[req.Path]
	prefix := ""
	if cur != nil {
		prefix = identifierPrefix(cur.text, req.Line, req.Col)
	}
	for _, b := range s.buffers {
		b.extract()
	}
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	var items []ilsp.CompletionItem
	add := func(words map[string]struct{}, tier int) {
		var ws []string
		for w := range words {
			if seen[w] || w == prefix || !matchesPrefix(w, prefix) {
				continue
			}
			ws = append(ws, w)
		}
		sort.Strings(ws)
		for _, w := range ws {
			if len(items) >= maxResults {
				return
			}
			seen[w] = true
			items = append(items, ilsp.CompletionItem{
				Label:        w,
				InsertText:   w,
				Kind:         protocol.KindText,
				SortText:     strconv.Itoa(tier) + strings.ToLower(w),
				LocalityTier: tier,
			})
		}
	}
	if cur != nil {
		add(cur.words, 0)
	}
	for path, b := range s.buffers {
		if path == req.Path {
			continue
		}
		add(b.words, 1)
	}
	add(s.project, 2)
	return items, nil
}

// matchesPrefix is the source-side pre-filter: case-insensitive prefix. The
// editor's fuzzy filter refines the survivors; an empty prefix (manual
// trigger at a word boundary) passes everything up to the cap.
func matchesPrefix(w, prefix string) bool {
	if prefix == "" {
		return true
	}
	if len(w) < len(prefix) {
		return false
	}
	return strings.EqualFold(w[:len(prefix)], prefix)
}

// identifierPrefix is the partial identifier ending at (line, col) in text.
func identifierPrefix(text string, line, col int) string {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	runes := []rune(lines[line])
	if col > len(runes) {
		col = len(runes)
	}
	start := col
	for start > 0 && isWordRune(runes[start-1]) {
		start--
	}
	return string(runes[start:col])
}

// extract refreshes a dirty buffer's word set.
func (b *buffer) extract() {
	if !b.dirty {
		return
	}
	b.words = extractWords(b.text, nil)
	b.dirty = false
}

// extractWords collects identifier words of at least minWordLen runes into
// dst (allocating when nil).
func extractWords(text string, dst map[string]struct{}) map[string]struct{} {
	if dst == nil {
		dst = map[string]struct{}{}
	}
	start := -1
	runes := []rune(text)
	for i, r := range runes {
		if isWordRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			addWord(dst, runes[start:i])
			start = -1
		}
	}
	if start >= 0 {
		addWord(dst, runes[start:])
	}
	return dst
}

func addWord(dst map[string]struct{}, w []rune) {
	if len(w) < minWordLen || unicode.IsDigit(w[0]) {
		return
	}
	dst[string(w)] = struct{}{}
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// scan walks root once, extracting words from every plausible text file
// within the size/count caps into the shared project set.
func (s *Source) scan(root string) {
	words := map[string]struct{}{}
	files := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (skipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if files >= maxScanFiles {
			return filepath.SkipAll
		}
		if info, err := d.Info(); err != nil || info.Size() > maxFileSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || looksBinary(data) {
			return nil
		}
		files++
		extractWords(string(data), words)
		return nil
	})
	s.mu.Lock()
	s.project = words
	s.scanned = true
	s.mu.Unlock()
}

// looksBinary reports a NUL byte in the head — good enough to skip binaries.
func looksBinary(data []byte) bool {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// ScanDone reports whether the one-shot project scan finished (tests).
func (s *Source) ScanDone() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scanned
}

// Package words is the word-index completion source (Roadmap 0410, #852):
// vim-keyword-level completion from identifier words seen in open buffers and
// a lazy, per-language background scan of the project tree. It is instant,
// needs no server round-trip, and rescues the popup when a language server is
// slow, missing, or dead.
//
// What counts as a word (#2652): identifiers in *code* of the *same language*
// as the request. The highlight layer's segments (highlight.Segments) mask
// strings, comments and chars and attribute every embedded fragment to its
// own language, so `"stringWord"` and `// commentWord` never become
// candidates, Markdown prose is not indexed at all, and `$myObj` in a ```php
// fence of README.md lands in the PHP index — offered in a .php buffer and
// inside the fence, never in a Python one. A buffer whose language has no
// grammar (Plain Text, a disabled plugin, a no-cgo build) keeps the plain
// tokenizer over its own text, so a plain-text buffer still completes; such
// files contribute nothing to the project tier.
//
// Freshness: open buffers update incrementally — the engine forwards every
// EditorChange event (full text) and the word set re-extracts lazily on the
// next query. The project index scans one language at a time, on the first
// request from a buffer of that language, and re-extracts files the watcher
// reports through Engine.NotifyFileChanged.
package words

import (
	"context"
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
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// Scan limits: the index is a convenience, not a database — bound the work.
const (
	maxFileSize  = 256 << 10 // per-file byte cap for the project scan
	maxScanFiles = 10000     // per-language project-scan file cap
	maxResults   = 200       // per-query item cap (the editor fuzzy-filters further)
	minWordLen   = 3         // shorter identifiers are noise
)

// wordSet is one language's words from one text.
type wordSet = map[string]struct{}

// Source is the word index. It implements complete.Source,
// complete.EventObserver and complete.FileObserver.
type Source struct {
	mu      sync.RWMutex
	buffers map[string]*buffer // open-buffer text + lazily extracted words
	project *langindex.Index[wordSet]
}

// buffer is one observed open buffer. lang is the name the buffer's language
// resolves by (#2048) — stored, since a "Treat Buffer as …" switch changes
// which tokenizer applies. gen counts Observe updates (#2193): the query
// snapshots it before tokenizing outside the lock, and only a matching gen
// on re-install may clear dirty — an edit that landed mid-extraction keeps
// the buffer dirty for the next query.
type buffer struct {
	text  string
	lang  string
	words map[string]wordSet // per language id
	dirty bool
	gen   uint64
}

// New returns the source over the project at root ("" indexes no files).
// Nothing is scanned until a buffer of some language asks.
func New(root string) *Source {
	return &Source{
		buffers: map[string]*buffer{},
		project: langindex.New(root, langindex.Limits{MaxFileSize: maxFileSize, MaxFiles: maxScanFiles}, nil, extractFile),
	}
}

// Name implements complete.Source.
func (s *Source) Name() string { return "words" }

// Priority implements complete.Source: the word index loses every de-dup
// against the server and the symbol index.
func (s *Source) Priority() int { return ilsp.PriorityWords }

// CompletesIn implements complete.ContextSource (#2654): the word index is
// the one local source a comment position still dispatches, answering with
// the current buffer's words only — prose in a comment refers to the code
// around it, not to the project's every identifier.
func (s *Source) CompletesIn(ctx lang.CompletionContext) bool { return ctx == lang.CtxComment }

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
		delete(s.buffers, ev.BufKey())
		return
	}
	b := s.buffers[ev.BufKey()]
	if b == nil {
		b = &buffer{}
		s.buffers[ev.BufKey()] = b
	}
	if b.lang != ev.LangName() {
		b.lang, b.words = ev.LangName(), nil
	}
	b.text, b.dirty = ev.Text, true
	b.gen++
}

// InvalidateFile implements complete.FileObserver: an on-disk change
// re-extracts the file for every scanned language, queued behind the
// index's single worker (#2176).
func (s *Source) InvalidateFile(path string) { s.project.Invalidate(path) }

// Complete implements complete.Source: candidates are identifier words of the
// request's language family (complete.Request.Langs) hump-matched by the
// partial word at the request position — current buffer first, then other
// buffers, then the project index — capped at maxResults. The word being
// typed itself is excluded. SortText encodes the locality tier, so the
// merged popup lists nearer words first. The first query in a language
// starts that language's project scan; until it finishes the buffer tiers
// answer alone.
func (s *Source) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	// Tokenizing dirty buffers must not run under the lock (#2193): Observe is
	// reached synchronously from the UI's Update and would block behind a
	// re-tokenize of every dirty megabyte-sized text. Snapshot under the lock,
	// extract unlocked, re-install — a buffer edited mid-extraction stays
	// dirty (gen moved) and re-extracts on the next query; one whose
	// language switched discards the wrong-tokenizer result.
	type extraction struct {
		b          *buffer
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
	for _, b := range s.buffers {
		if b.dirty {
			jobs = append(jobs, extraction{b: b, lang: b.lang, text: b.text, gen: b.gen})
		}
	}
	s.mu.Unlock()

	prefix := identifierPrefix(curText, req.Line, req.Col)
	for i := range jobs {
		words := extractBuffer(jobs[i].lang, jobs[i].text)
		s.mu.Lock()
		if jobs[i].b.lang == jobs[i].lang {
			jobs[i].b.words = words
			if jobs[i].b.gen == jobs[i].gen {
				jobs[i].b.dirty = false
			}
		}
		s.mu.Unlock()
	}

	langs := req.Langs()
	s.project.Ensure(langs...)

	mode := completionCase()
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	var items []ilsp.CompletionItem
	add := func(sets []wordSet, tier int) {
		var ws []string
		for _, set := range sets {
			for w := range set {
				if seen[w] || w == prefix || !matchesPrefix(w, prefix, mode) {
					continue
				}
				seen[w] = true
				ws = append(ws, w)
			}
		}
		sort.Strings(ws)
		for _, w := range ws {
			if len(items) >= maxResults {
				return
			}
			items = append(items, ilsp.CompletionItem{
				Label:        w,
				InsertText:   w,
				Kind:         protocol.KindText,
				SortText:     strconv.Itoa(tier) + strings.ToLower(w),
				LocalityTier: tier,
			})
		}
	}
	pick := func(b *buffer) []wordSet {
		var sets []wordSet
		for _, l := range langs {
			if set := b.words[l]; len(set) > 0 {
				sets = append(sets, set)
			}
		}
		return sets
	}
	if cur != nil {
		add(pick(cur), 0)
	}
	if req.Context == lang.CtxComment {
		return items, nil // a comment offers the current buffer's words only (#2654)
	}
	var others []wordSet
	for key, b := range s.buffers {
		if key != req.BufKey() {
			others = append(others, pick(b)...)
		}
	}
	add(others, 1)
	var project []wordSet
	s.project.Each(langs, func(_ string, set wordSet) { project = append(project, set) })
	add(project, 2)
	return items, nil
}

// extractBuffer tokenizes an open buffer per language: code segments of its
// language and its embedded fragments when the language has a grammar; the
// whole text under the buffer's own language id ("" for Plain Text) when it
// has none — a plain-text buffer would otherwise have no completion.
func extractBuffer(langName, text string) map[string]wordSet {
	id := langindex.LangOf(langName)
	out := map[string]wordSet{}
	if l, ok := lang.ByID(id); !ok || l.Grammar == nil {
		out[id] = extractWords(text, nil)
		return out
	}
	lines := strings.Split(text, "\n")
	for _, seg := range highlight.Segments(id, lines, nil) {
		out[seg.Lang] = extractWords(seg.CodeText(lines), out[seg.Lang])
	}
	return out
}

// extractFile is the project index's extractor: code segments only — a file
// whose language has no grammar contributes nothing.
func extractFile(_, host, text string, only func(string) bool) map[string]wordSet {
	if host == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	out := map[string]wordSet{}
	for _, seg := range highlight.Segments(host, lines, only) {
		out[seg.Lang] = extractWords(seg.CodeText(lines), out[seg.Lang])
	}
	return out
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

// matchesPrefix is the source-side pre-filter (#2650): the same JetBrains-style
// hump match the popup applies, under completion.case_sensitivity, so "gur"
// reaches GotoURLResolver from the local index rather than only from a
// language server. The editor re-ranks the survivors; an empty prefix (manual
// trigger at a word boundary) passes everything up to the cap.
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

// extractWords collects identifier words of at least minWordLen runes into
// dst (allocating when nil).
func extractWords(text string, dst wordSet) wordSet {
	if dst == nil {
		dst = wordSet{}
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

func addWord(dst wordSet, w []rune) {
	if len(w) < minWordLen || unicode.IsDigit(w[0]) {
		return
	}
	dst[string(w)] = struct{}{}
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

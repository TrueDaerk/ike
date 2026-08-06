// Package highlight is the syntax-highlighting engine. It no longer knows any
// specific language: the set of languages lives in the internal/lang registry,
// populated by per-language plugins (plugins/languages/*). This package compiles
// and runs Tree-sitter grammars (behind the cgo build tag) and resolves capture
// names to theme colours; a language's grammar is an opaque lang.Grammar token
// built via NewGrammar.
package highlight

import (
	"strings"

	"ike/internal/lang"
)

// Lang returns the language id for a path, or "" when no language matches.
func Lang(path string) string {
	if l, ok := lang.ByPath(path); ok {
		return l.ID
	}
	return ""
}

// Supported reports whether a path has a language that can produce spans — a
// Tree-sitter grammar or a Go span producer (#1585) — so the editor only
// schedules a parse when it will produce spans.
func Supported(path string) bool {
	l, ok := lang.ByPath(path)
	return ok && (l.Grammar != nil || l.Spans != nil || l.Lint != nil)
}

// Lint runs the language's Go-computed linter (#1623) over lines and returns
// its notes; nil when the path has no language or the language registers no
// Lint. It rides the same schedule as the parse — the editor calls both in one
// command — so a linted language needs no separate pipeline.
func Lint(path string, lines []string) []lang.Note {
	l, ok := lang.ByPath(path)
	if !ok || l.Lint == nil {
		return nil
	}
	return l.Lint(lines)
}

// Highlight parses lines with the grammar for path and returns the spans,
// including spans for embedded-language fragments (SQL in a Python string, …)
// detected by the host grammar's injection query and parsed with the fragment
// language's own grammar (issue #299). It returns nil when the path has no
// language, no grammar, or the build has CGo disabled (the stub). The actual
// parse lives in parse_cgo.go / parse_stub.go.
func Highlight(path string, lines []string) []Span {
	spans, _, _ := HighlightScoped(path, lines)
	return spans
}

// HighlightScoped is Highlight plus sticky-scroll scopes (#168) and fold
// ranges (#144): the same single parse also collects the multi-line nodes
// whose kinds the language registers as ScopeNodes / FoldNodes, in pre-order.
// A language without FoldNodes falls back to its ScopeNodes, so every language
// with sticky scopes is foldable; both nil means the feature is simply inert.
func HighlightScoped(path string, lines []string) ([]Span, []Scope, []Fold) {
	l, ok := lang.ByPath(path)
	if !ok || (l.Grammar == nil && l.Spans == nil) {
		return nil, nil, nil
	}
	var spans []Span
	var scopes []Scope
	var folds []Fold
	if l.Grammar != nil {
		spans, scopes, folds = parseScoped(l.Grammar, l.ScopeNodes, foldKinds(l), lines)
		var injected []Fold
		spans, injected = overlayFragments(l, lines, spans)
		// An embedded region folds by its own language's rules (#1329): a
		// .http request body that is JSON collapses like a JSON buffer's
		// objects do.
		folds = append(folds, injected...)
	}
	// Rainbow brackets (#789, #1628): bracket depth comes from the pure-Go
	// pair tracker, masked by the string/comment captures the grammar just
	// produced. The spans go before the grammar's — CaptureAt is
	// first-covering-wins, and grammars capture the same tokens as
	// punctuation — but after the Go-produced spans below, so a language that
	// styles a cell itself (a csv column) keeps its colour.
	if b := bracketSpans(l, lines, spans); len(b) > 0 {
		spans = append(b, spans...)
	}
	if l.Spans != nil {
		// Go-produced spans (#1585) are prepended so they win over the
		// grammar's coarser captures (the whole url node is one @string) —
		// the same trick overlayFragments uses for injected fragments.
		spans = append(langSpans(l.Spans(lines)), spans...)
	}
	return spans, scopes, folds
}

// langSpans converts registry-level Go spans (#1585) to highlight spans.
func langSpans(in []lang.Span) []Span {
	out := make([]Span, len(in))
	for i, s := range in {
		out[i] = Span{Line: s.Line, StartCol: s.StartCol, EndCol: s.EndCol, Capture: s.Capture, Replace: s.Replace}
	}
	return out
}

// HighlightFenced parses lines tagged with a markdown fence info string (as in
// "```go") and returns the spans. The tag is resolved as a language id first,
// then as a file extension ("py"), since hover markdown uses either. It returns
// nil when the tag resolves to no language or the language has no grammar.
func HighlightFenced(tag string, lines []string) []Span {
	l, ok := fencedLang(tag)
	if !ok || l.Grammar == nil {
		return nil
	}
	spans := parse(l.Grammar, lines)
	// Fenced text is read like buffer text, so it gets the same bracket
	// colouring (#1628) — a JSON body in the HTTP response pane nests exactly
	// as it would in a buffer.
	if b := bracketSpans(l, lines, spans); len(b) > 0 {
		spans = append(b, spans...)
	}
	return spans
}

// FencedFolds parses lines tagged with a fence info string and returns their
// foldable ranges (#1330) — the fenced counterpart of HighlightScoped's folds,
// for viewers that show text they know only by content type: the HTTP response
// pane folds a JSON body's objects the same way an editor buffer folds them.
// nil when the tag resolves to no language, no grammar or no fold kinds.
func FencedFolds(tag string, lines []string) []Fold {
	l, ok := fencedLang(tag)
	if !ok || l.Grammar == nil {
		return nil
	}
	kinds := foldKinds(l)
	if len(kinds) == 0 {
		return nil
	}
	_, _, folds := parseScoped(l.Grammar, nil, kinds, lines)
	return folds
}

// FencedSupported reports whether a fence tag resolves to a language with a
// compiled-in grammar, i.e. whether HighlightFenced can produce spans at all.
// Consumers use it to say *why* a body renders plain (#1270): in a CGo-free
// build (or one without the grammar plugin linked) HighlightFenced returns
// nil silently.
func FencedSupported(tag string) bool {
	l, ok := fencedLang(tag)
	return ok && l.Grammar != nil
}

// fencedLang resolves a fence tag as a language id first, then an extension.
func fencedLang(tag string) (lang.Language, bool) {
	l, ok := lang.ByID(strings.ToLower(tag))
	if !ok {
		l, ok = lang.ByExt(tag)
	}
	return l, ok
}

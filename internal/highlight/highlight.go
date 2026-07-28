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

// Supported reports whether a path has a language with a highlighting grammar, so
// the editor only schedules a parse when it will produce spans.
func Supported(path string) bool {
	l, ok := lang.ByPath(path)
	return ok && l.Grammar != nil
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
	if !ok || l.Grammar == nil {
		return nil, nil, nil
	}
	spans, scopes, folds := parseScoped(l.Grammar, l.ScopeNodes, foldKinds(l), lines)
	spans, injected := overlayFragments(l, lines, spans)
	// An embedded region folds by its own language's rules (#1329): a .http
	// request body that is JSON collapses like a JSON buffer's objects do.
	return spans, scopes, append(folds, injected...)
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
	return parse(l.Grammar, lines)
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

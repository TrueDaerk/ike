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
	l, ok := lang.ByPath(path)
	if !ok || l.Grammar == nil {
		return nil
	}
	return overlayFragments(l.Grammar, lines, parse(l.Grammar, lines))
}

// HighlightFenced parses lines tagged with a markdown fence info string (as in
// "```go") and returns the spans. The tag is resolved as a language id first,
// then as a file extension ("py"), since hover markdown uses either. It returns
// nil when the tag resolves to no language or the language has no grammar.
func HighlightFenced(tag string, lines []string) []Span {
	l, ok := lang.ByID(strings.ToLower(tag))
	if !ok {
		l, ok = lang.ByExt(tag)
	}
	if !ok || l.Grammar == nil {
		return nil
	}
	return parse(l.Grammar, lines)
}

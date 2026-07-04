// Package highlight is the syntax-highlighting engine. It no longer knows any
// specific language: the set of languages lives in the internal/lang registry,
// populated by per-language plugins (plugins/languages/*). This package compiles
// and runs Tree-sitter grammars (behind the cgo build tag) and resolves capture
// names to theme colours; a language's grammar is an opaque lang.Grammar token
// built via NewGrammar.
package highlight

import "ike/internal/lang"

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

// Highlight parses lines with the grammar for path and returns the spans. It
// returns nil when the path has no language, no grammar, or the build has CGo
// disabled (the stub). The actual parse lives in parse_cgo.go / parse_stub.go.
func Highlight(path string, lines []string) []Span {
	l, ok := lang.ByPath(path)
	if !ok || l.Grammar == nil {
		return nil
	}
	return parse(l.Grammar, lines)
}

//go:build !cgo

package highlight

import "ike/internal/lang"

// parse is the no-op fallback for CGo-disabled builds: highlighting is simply
// off, the editor renders plain text, and everything else still compiles and
// cross-compiles. The real Tree-sitter parser is in parse_cgo.go.
func parse(g lang.Grammar, lines []string) []Span { return nil }

// parseScoped is the matching no-op fallback for the scope-collecting parse
// (sticky scroll, #168); without CGo there is no tree to walk.
func parseScoped(g lang.Grammar, scopeKinds []string, lines []string) ([]Span, []Scope) {
	return nil, nil
}

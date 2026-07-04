//go:build !cgo

package highlight

import "ike/internal/lang"

// parse is the no-op fallback for CGo-disabled builds: highlighting is simply
// off, the editor renders plain text, and everything else still compiles and
// cross-compiles. The real Tree-sitter parser is in parse_cgo.go.
func parse(g lang.Grammar, lines []string) []Span { return nil }

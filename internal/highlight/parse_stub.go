//go:build !cgo

package highlight

import (
	"ike/internal/lang"
	"ike/internal/structval"
)

// parse is the no-op fallback for CGo-disabled builds: highlighting is simply
// off, the editor renders plain text, and everything else still compiles and
// cross-compiles. The real Tree-sitter parser is in parse_cgo.go.
func parse(g lang.Grammar, lines []string) []Span { return nil }

// parseScoped is the matching no-op fallback for the scope- and fold-collecting
// parse (sticky scroll #168, code folding #144); without CGo there is no tree
// to walk.
func parseScoped(g lang.Grammar, scopeKinds, foldKinds []string, lines []string) ([]Span, []Scope, []Fold) {
	return nil, nil, nil
}

// SelectionRangesAt is the matching no-op fallback for the Tree-sitter
// extend-selection ladder (#1912); without CGo there is no tree to walk, and
// the editor falls through to its word/line/buffer ladder.
func SelectionRangesAt(path string, lines []string, line, col int) []NodeRange { return nil }

// ExpressionEndingAt is the matching no-op fallback for the postfix-completion
// expression finder (#1913); without CGo there is no tree, and the postfix
// source falls back to its token heuristic.
func ExpressionEndingAt(path string, lines []string, line, col int, kinds []string) (NodeRange, bool) {
	return NodeRange{}, false
}

// SyntaxChainAt is the matching no-op fallback for the structural-value chain
// (gy / gY, #2499); without CGo there is no tree, and both commands report
// that there is no value under the cursor.
func SyntaxChainAt(path string, lines []string, line, col int) []structval.Node { return nil }

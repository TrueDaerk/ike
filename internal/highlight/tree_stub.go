//go:build !cgo

package highlight

// SyntaxTree is the matching no-op fallback for the syntax-tree snapshot
// (#2667); without CGo there is no tree, and a structural index (the PHP
// declaration index) stays empty and reports itself unavailable.
func SyntaxTree(langID string, lines []string) *SyntaxNode { return nil }

//go:build cgo

package langsql

import (
	tssql "github.com/DerekStride/tree-sitter-sql/bindings/go"
	ts "github.com/tree-sitter/go-tree-sitter"
)

// parseHasErrors runs the Tree-sitter SQL parse as the formatter's validity
// gate (#1403): checked is true when a verdict was possible, bad when the
// tree contains error nodes — malformed SQL is left untouched, never mangled.
func parseHasErrors(text string) (bad, checked bool) {
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(ts.NewLanguage(tssql.Language())); err != nil {
		return false, false
	}
	tree := parser.Parse([]byte(text), nil)
	if tree == nil {
		return false, false
	}
	defer tree.Close()
	return tree.RootNode().HasError(), true
}

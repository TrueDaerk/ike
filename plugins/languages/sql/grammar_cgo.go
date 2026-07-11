//go:build cgo

package langsql

import (
	tssql "github.com/DerekStride/tree-sitter-sql/bindings/go"
	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// grammar builds the SQL highlighting grammar from the Tree-sitter SQL binding
// and the embedded highlights query. The !cgo stub returns nil.
func grammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(tssql.Language()), query)
}

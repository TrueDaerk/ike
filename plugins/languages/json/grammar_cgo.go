//go:build cgo

package langjson

import (
	ts "github.com/tree-sitter/go-tree-sitter"
	tsjson "github.com/tree-sitter/tree-sitter-json/bindings/go"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// grammar builds the JSON highlighting grammar from the Tree-sitter JSON
// binding and the embedded highlights query. The !cgo stub returns nil.
func grammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(tsjson.Language()), query)
}

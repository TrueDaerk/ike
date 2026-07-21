//go:build cgo

package langtoml

import (
	ts "github.com/tree-sitter/go-tree-sitter"
	tstoml "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// grammar builds the TOML highlighting grammar from the Tree-sitter TOML
// binding and the embedded highlights query. The !cgo stub returns nil.
func grammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(tstoml.Language()), query)
}

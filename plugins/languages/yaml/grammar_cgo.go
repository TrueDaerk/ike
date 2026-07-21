//go:build cgo

package langyaml

import (
	ts "github.com/tree-sitter/go-tree-sitter"
	tsyaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// grammar builds the YAML highlighting grammar from the Tree-sitter YAML
// binding and the embedded highlights query. The !cgo stub returns nil.
func grammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(tsyaml.Language()), query)
}

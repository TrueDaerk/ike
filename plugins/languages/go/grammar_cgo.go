//go:build cgo

package langgo

import (
	ts "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// grammar builds the Go highlighting grammar from the Tree-sitter Go binding and
// the embedded highlights query. The !cgo stub returns nil.
func grammar() lang.Grammar {
	return highlight.NewGrammarInjections(ts.NewLanguage(tsgo.Language()), query, injections)
}

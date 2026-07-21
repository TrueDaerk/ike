//go:build cgo

package langshell

import (
	ts "github.com/tree-sitter/go-tree-sitter"
	tsbash "github.com/tree-sitter/tree-sitter-bash/bindings/go"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// grammar builds the Shell highlighting grammar from the Tree-sitter bash
// binding and the embedded highlights query. The !cgo stub returns nil.
func grammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(tsbash.Language()), query)
}

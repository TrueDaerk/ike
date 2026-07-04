//go:build cgo

package langpython

import (
	ts "github.com/tree-sitter/go-tree-sitter"
	tspy "github.com/tree-sitter/tree-sitter-python/bindings/go"

	"ike/internal/highlight"
	"ike/internal/lang"
)

func grammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(tspy.Language()), query)
}

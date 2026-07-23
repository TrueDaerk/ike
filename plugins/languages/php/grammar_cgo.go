//go:build cgo

package langphp

import (
	ts "github.com/tree-sitter/go-tree-sitter"
	tsphp "github.com/tree-sitter/tree-sitter-php/bindings/go"

	"ike/internal/highlight"
	"ike/internal/lang"
)

func grammar() lang.Grammar {
	return highlight.NewGrammarInjections(ts.NewLanguage(tsphp.LanguagePHP()), query, injections)
}

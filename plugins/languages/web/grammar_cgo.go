//go:build cgo

package langweb

import (
	ts "github.com/tree-sitter/go-tree-sitter"
	tscss "github.com/tree-sitter/tree-sitter-css/bindings/go"
	tshtml "github.com/tree-sitter/tree-sitter-html/bindings/go"
	tstsx "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// tsGrammar builds the one grammar serving every JS/TS dialect (#925): the
// TSX grammar is the permissive superset — it parses plain JavaScript, JSX
// and TypeScript annotations alike. A single grammar keeps the single
// "typescript" language id, and with it a single vtsls instance per project
// (splitting js/tsx into their own ids would spawn one server per id and
// break unsaved cross-file intelligence between .ts and .tsx). The one
// casualty: legacy angle-bracket type assertions (`<T>x` instead of `x as T`)
// misparse, which upstream accepts for the same reason in .tsx.
// The !cgo stubs return nil.
func tsGrammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(tstsx.LanguageTSX()), tsQuery)
}

// htmlGrammar carries the injection query: <script> and <style> bodies parse
// with the typescript/css grammars through the fragment seam.
func htmlGrammar() lang.Grammar {
	return highlight.NewGrammarInjections(ts.NewLanguage(tshtml.Language()), htmlQuery, htmlInjections)
}

func cssGrammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(tscss.Language()), cssQuery)
}

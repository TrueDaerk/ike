//go:build cgo

package highlight

import (
	"sync"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/lang"
)

// grammarImpl is the concrete lang.Grammar: a Tree-sitter language plus its
// highlight query source. The query is compiled lazily and once (a language plugin
// builds many grammars at init, but only the opened ones ever parse), and a query
// that fails to compile — grammar/query version skew — disables highlighting for
// that language rather than crashing.
type grammarImpl struct {
	lang     *ts.Language
	querySrc string

	once  sync.Once
	query *ts.Query
	ok    bool
}

// NewGrammar builds a highlighting grammar from a compiled Tree-sitter language
// and its highlights.scm query source. Language plugins call it from a cgo-tagged
// file; the returned token is stored in lang.Language.Grammar. The matching stub
// (grammar_stub.go) returns nil for CGO_ENABLED=0 builds.
func NewGrammar(l *ts.Language, query string) lang.Grammar {
	return &grammarImpl{lang: l, querySrc: query}
}

// compiled lazily compiles the query, returning ok=false if it will not compile.
func (g *grammarImpl) compiled() (*ts.Language, *ts.Query, bool) {
	g.once.Do(func() {
		q, err := ts.NewQuery(g.lang, g.querySrc)
		if err != nil {
			return
		}
		g.query, g.ok = q, true
	})
	return g.lang, g.query, g.ok
}

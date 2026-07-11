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
	lang         *ts.Language
	querySrc     string
	injectionSrc string

	once  sync.Once
	query *ts.Query
	ok    bool

	injOnce  sync.Once
	injQuery *ts.Query
	injOK    bool
}

// NewGrammar builds a highlighting grammar from a compiled Tree-sitter language
// and its highlights.scm query source. Language plugins call it from a cgo-tagged
// file; the returned token is stored in lang.Language.Grammar. The matching stub
// (grammar_stub.go) returns nil for CGO_ENABLED=0 builds.
func NewGrammar(l *ts.Language, query string) lang.Grammar {
	return &grammarImpl{lang: l, querySrc: query}
}

// NewGrammarInjections is NewGrammar plus an injections.scm query source for
// embedded-language fragment detection (see Fragments). An empty injections
// source behaves exactly like NewGrammar.
func NewGrammarInjections(l *ts.Language, query, injections string) lang.Grammar {
	return &grammarImpl{lang: l, querySrc: query, injectionSrc: injections}
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

// compiledInjections lazily compiles the injection query, returning ok=false
// when the grammar has none or it will not compile.
func (g *grammarImpl) compiledInjections() (*ts.Language, *ts.Query, bool) {
	g.injOnce.Do(func() {
		if g.injectionSrc == "" {
			return
		}
		q, err := ts.NewQuery(g.lang, g.injectionSrc)
		if err != nil {
			return
		}
		g.injQuery, g.injOK = q, true
	})
	return g.lang, g.injQuery, g.injOK
}

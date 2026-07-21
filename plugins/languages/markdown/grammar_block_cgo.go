//go:build cgo

package langmarkdown

// The markdown block grammar (tree-sitter-grammars/tree-sitter-markdown
// v0.5.3, MIT — see grammar/LICENSE) is vendored as C source: upstream ships
// node/python/rust/swift bindings but none for Go. Block and inline grammar
// live in separate .go files on purpose — each cgo preamble is its own
// translation unit, so the two parsers' file-scope statics cannot collide.

/*
#cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}/grammar/block
#include "grammar/block/parser.c"
#include "grammar/block/scanner.c"
*/
import "C"

import (
	"unsafe"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// blockGrammar builds the markdown block grammar with its injection query —
// the query that hands (inline) nodes to the inline grammar and fenced code
// to the language named by the fence. The !cgo stub returns nil.
func blockGrammar() lang.Grammar {
	return highlight.NewGrammarInjections(ts.NewLanguage(unsafe.Pointer(C.tree_sitter_markdown())), blockQuery, injectionsQuery)
}

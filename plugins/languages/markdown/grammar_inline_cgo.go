//go:build cgo

package langmarkdown

// Inline half of the vendored markdown grammar — see grammar_block_cgo.go for
// why the source is vendored and why block/inline sit in separate files.

/*
#cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}/grammar/inline
#include "grammar/inline/parser.c"
#include "grammar/inline/scanner.c"
*/
import "C"

import (
	"unsafe"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// inlineGrammar builds the markdown inline grammar (emphasis, code spans,
// links). The !cgo stub returns nil.
func inlineGrammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(unsafe.Pointer(C.tree_sitter_markdown_inline())), inlineQuery)
}

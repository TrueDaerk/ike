//go:build cgo

package langmake

// The Makefile grammar (alemuller/tree-sitter-make, main @ a4b9187, MIT — see
// grammar/LICENSE) is vendored as C source under grammar/: upstream ships no
// Go binding at all, so vendoring parser.c the way the Dockerfile and gomod
// grammars are vendored is the only route. The grammar has no external
// scanner — parser.c is the whole translation unit. The usual cgo/stub split
// stays intact.

/*
#cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}/grammar
#include "grammar/parser.c"
*/
import "C"

import (
	"unsafe"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// grammar builds the Makefile highlighting grammar from the vendored parser,
// the embedded highlights query and the shell-injection query (recipe bodies
// parse with the bash grammar, #1136). The !cgo stub returns nil.
func grammar() lang.Grammar {
	return highlight.NewGrammarInjections(ts.NewLanguage(unsafe.Pointer(C.tree_sitter_make())), query, injections)
}

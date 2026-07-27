//go:build cgo

package langxml

// The XML grammar (tree-sitter-grammars/tree-sitter-xml, master @ 5000ae8,
// MIT — see grammar/LICENSE) is vendored as C source under grammar/: upstream
// ships its Go binding inside the grammar repo behind a nested go.mod, so
// vendoring parser.c/scanner.c the way the Dockerfile and Makefile grammars
// are vendored is the practical route. Only the repo's `xml` grammar is
// vendored — its sibling `dtd` grammar is out of scope (#1253). The external
// scanner includes the repo's shared common/scanner.h, kept at
// grammar/common/scanner.h with the include path adjusted accordingly (the
// upstream layout puts it two directories up, which does not survive
// flattening). The usual cgo/stub split stays intact.

/*
#cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}/grammar
#include "grammar/parser.c"
#include "grammar/scanner.c"
*/
import "C"

import (
	"unsafe"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// grammar builds the XML highlighting grammar from the vendored parser and
// the embedded highlights query. The !cgo stub returns nil.
func grammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(unsafe.Pointer(C.tree_sitter_xml())), query)
}

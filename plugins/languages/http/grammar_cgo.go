//go:build cgo

package langhttp

// The HTTP grammar (rest-nvim/tree-sitter-http, MIT — see grammar/LICENSE) is
// vendored as C source under grammar/: upstream publishes no importable Go
// binding module. Including parser.c from the cgo preamble is exactly what
// the official grammar bindings do; the usual cgo/stub split stays intact.

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

// grammar builds the .http highlighting grammar from the vendored parser and
// the embedded highlights query. The !cgo stub returns nil.
func grammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(unsafe.Pointer(C.tree_sitter_http())), query)
}

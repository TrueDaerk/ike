//go:build cgo

package langdockerfile

// The Dockerfile grammar (camdencheek/tree-sitter-dockerfile v0.2.0, MIT — see
// grammar/LICENSE) is vendored as C source under grammar/: upstream's Go
// binding directory carries a nested go.mod whose module path equals the repo
// root, so it is not importable as a Go module. Including parser.c/scanner.c
// from the cgo preamble is exactly what the official grammar bindings do; the
// usual cgo/stub split stays intact.

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

// grammar builds the Dockerfile highlighting grammar from the vendored parser
// and the embedded highlights query. The !cgo stub returns nil.
func grammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(unsafe.Pointer(C.tree_sitter_dockerfile())), query)
}

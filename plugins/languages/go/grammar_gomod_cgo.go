//go:build cgo

package langgo

// The go.mod grammar (camdencheek/tree-sitter-go-mod, main @ 2e88687, MIT —
// see grammar/LICENSE) is vendored as C source under grammar/: upstream's
// go.mod declares the module path github.com/tree-sitter/tree-sitter-gomod,
// which does not match the repository, so it is not importable as a Go module
// (same situation as the Dockerfile grammar by the same author). Vendored from
// main rather than the v1.1.0 tag because main adds the `tool` and `ignore`
// directives (Go 1.24/1.25). This lives in its own file so the cgo preamble is
// its own translation unit — the Go grammar in grammar_cgo.go comes from a
// module binding and cannot collide, but the split keeps the pattern uniform
// with the markdown plugin.

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

// gomodGrammar builds the go.mod/go.work highlighting grammar from the
// vendored parser and the embedded highlights query. The !cgo stub returns nil.
func gomodGrammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(unsafe.Pointer(C.tree_sitter_gomod())), gomodQuery)
}

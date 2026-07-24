//go:build cgo

package langgo

// The go.work grammar (omertuc/tree-sitter-go-work, main @ 949a8a4, MIT — see
// grammar_gowork/LICENSE) is vendored as C source under grammar_gowork/, the
// same pinned-sha pattern as the gomod grammar (#1119): upstream ships no Go
// binding, and the gomod grammar has no `use` directive, so go.work `use`
// lines fell into error recovery and rendered uncolored. Separate directory,
// separate cgo file: each parser.c is its own translation unit, mirroring how
// grammar_gomod_cgo.go is split from grammar_cgo.go. parser.c is regenerated
// from upstream's pinned src/grammar.json with tree-sitter-cli 0.25.10 (the
// version the vendored gomod parser was generated with): cgo merges C type
// definitions across a package, so both grammars must share one
// tree_sitter/parser.h generation — upstream's 2022-era ABI-13 parser.c does
// not compile against it. Known trade-off: this
// grammar predates Go 1.21's `toolchain` directive, so a `toolchain` line in
// go.work now error-recovers instead — `use` blocks are by far the dominant
// content of go.work files, so the trade goes the right way.

/*
#cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}/grammar_gowork
#include "grammar_gowork/parser.c"
*/
import "C"

import (
	"unsafe"

	ts "github.com/tree-sitter/go-tree-sitter"

	"ike/internal/highlight"
	"ike/internal/lang"
)

// goworkGrammar builds the go.work highlighting grammar from the vendored
// parser and the embedded highlights query. The !cgo stub returns nil.
func goworkGrammar() lang.Grammar {
	return highlight.NewGrammar(ts.NewLanguage(unsafe.Pointer(C.tree_sitter_gowork())), goworkQuery)
}

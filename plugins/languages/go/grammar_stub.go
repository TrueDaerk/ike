//go:build !cgo

package langgo

import "ike/internal/lang"

// grammar returns nil for CGo-disabled builds: Go still registers (for LSP), just
// without highlighting. The real grammar is in grammar_cgo.go.
func grammar() lang.Grammar { return nil }

// gomodGrammar returns nil for CGo-disabled builds: go.mod/go.work still
// register (for LSP), just without highlighting. The real grammar is in
// grammar_gomod_cgo.go.
func gomodGrammar() lang.Grammar { return nil }

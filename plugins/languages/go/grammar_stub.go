//go:build !cgo

package langgo

import "ike/internal/lang"

// grammar returns nil for CGo-disabled builds: Go still registers (for LSP), just
// without highlighting. The real grammar is in grammar_cgo.go.
func grammar() lang.Grammar { return nil }

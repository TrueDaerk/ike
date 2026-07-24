//go:build !cgo

package langmake

import "ike/internal/lang"

// grammar returns nil for CGo-disabled builds: Makefiles still register (for
// filename detection, comments and the tab-indent default), just without
// highlighting. The real grammar is in grammar_cgo.go.
func grammar() lang.Grammar { return nil }

//go:build !cgo

package langxml

import "ike/internal/lang"

// grammar returns nil for CGo-disabled builds: XML files still register (for
// extension detection and the <!-- --> comment toggle), just without
// highlighting. The real grammar is in grammar_cgo.go.
func grammar() lang.Grammar { return nil }

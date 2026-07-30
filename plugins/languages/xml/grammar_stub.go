//go:build !cgo

package langxml

import "ike/internal/lang"

// grammar returns nil for CGo-disabled builds: XML files still register (for
// extension detection and the <!-- --> comment toggle), just without
// highlighting. The real grammar is in grammar_cgo.go.
func grammar() lang.Grammar { return nil }

// xmlParseHasErrors is the CGo-less stub: no tree, no verdict — the
// formatter's own parser still gates malformed documents (#1404).
func xmlParseHasErrors(text string) (bad, checked bool) { return false, false }

//go:build !cgo

package highlight

import "ike/internal/lang"

// detectFragments is the no-op fallback for CGo-disabled builds: no Tree-sitter
// means no injection queries, so buffers simply have no embedded fragments.
func detectFragments(g lang.Grammar, lines []string) []Fragment { return nil }

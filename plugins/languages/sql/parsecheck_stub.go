//go:build !cgo

package langsql

// parseHasErrors is the CGo-less stub: no tree, no verdict — the lexer-level
// checks (unterminated tokens, unbalanced parentheses) still gate.
func parseHasErrors(text string) (bad, checked bool) { return false, false }

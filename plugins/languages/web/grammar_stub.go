//go:build !cgo

package langweb

import "ike/internal/lang"

func tsGrammar() lang.Grammar   { return nil }
func htmlGrammar() lang.Grammar { return nil }
func cssGrammar() lang.Grammar  { return nil }

//go:build !cgo

package langmarkdown

import "ike/internal/lang"

func blockGrammar() lang.Grammar  { return nil }
func inlineGrammar() lang.Grammar { return nil }

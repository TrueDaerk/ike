//go:build !cgo

package highlight

import "ike/internal/lang"

// NewGrammar is the no-op fallback for CGo-disabled builds: it returns a nil
// grammar so registered languages simply have no highlighting and everything still
// compiles and cross-compiles. It takes `any` (not *ts.Language) because the
// Tree-sitter binding is a CGo package that cannot be imported without cgo; the
// real constructor is in grammar_cgo.go. Language plugins only call NewGrammar
// from their own cgo-tagged files, so this signature is never invoked.
func NewGrammar(l any, query string) lang.Grammar { return nil }

// NewGrammarInjections is the matching no-op fallback for NewGrammar's
// injection-aware variant (grammar_cgo.go).
func NewGrammarInjections(l any, query, injections string) lang.Grammar { return nil }

// Package langmake registers Makefile (#1136): Tree-sitter highlighting via
// the alemuller/tree-sitter-make grammar (vendored C source, see
// grammar_cgo.go) with shell injected into recipe bodies through the fragment
// seam (queries/injections.scm — the same mechanism HTML uses for
// <script>/<style>, #925), matched by exact base name (Makefile, makefile,
// GNUmakefile) plus the .mk extension. No LSP server: no mainstream Makefile
// language server exists, so it is highlighting-only. Make recipes require a
// literal tab — space-indented recipes are a syntax error — so the language
// declares UseTabs (#1137) and buffers indent with tabs regardless of
// editor.use_spaces (.editorconfig still overrides). Self-registers via
// init(); blank-imported in cmd/ike/main.go.
package langmake

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/highlights.scm
var query string

//go:embed queries/injections.scm
var injections string

// tabs is the per-language indent default (#1137): true = indent with tab
// characters. Declared as a variable because Language.UseTabs is a *bool
// (nil = no language opinion).
var tabs = true

func init() {
	register.Language(lang.Language{
		ID:          "make",
		Extensions:  []string{"mk"},
		Filenames:   []string{"Makefile", "makefile", "GNUmakefile"},
		Grammar:     grammar(),
		LineComment: "#",
		UseTabs:     &tabs,
		// Sticky scopes + folding (#168, #144): a rule's target line pins
		// while its recipe scrolls; rules, define bodies and conditionals
		// collapse.
		ScopeNodes: []string{"rule", "define_directive"},
		FoldNodes:  []string{"rule", "define_directive", "conditional"},
	})
}

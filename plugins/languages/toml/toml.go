// Package langtoml registers TOML (#895): Tree-sitter highlighting via the
// tree-sitter-grammars/tree-sitter-toml grammar and the taplo language server
// for completion — schema-store aware (Cargo.toml, pyproject.toml, … detected
// by filename), plus formatting and diagnostics. Directly relevant to IKE
// itself: the IKE config is TOML. Self-registers via init(); blank-imported in
// cmd/ike/main.go.
package langtoml

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/highlights.scm
var query string

func init() {
	register.Language(lang.Language{
		ID:         "toml",
		Extensions: []string{"toml"},
		Grammar:    grammar(),
		Server: &lang.ServerSpec{
			Language:    "toml",
			Command:     "taplo",
			Args:        []string{"lsp", "stdio"},
			RootMarkers: []string{".taplo.toml", "taplo.toml", ".git"},
			Install:     []string{"npm", "install", "-g", "@taplo/cli"},
		},
		LineComment: "#",
		// Sticky scopes + folding (#168, #144): [table] headers pin while
		// their pairs scroll; tables, arrays and inline tables fold.
		ScopeNodes: []string{"table", "table_array_element"},
		FoldNodes:  []string{"table", "table_array_element", "array", "inline_table"},
	})
}

// Package langyaml registers YAML (#879): Tree-sitter highlighting via the
// tree-sitter-grammars/tree-sitter-yaml grammar and Red Hat's
// yaml-language-server for completion — schema-store aware (Kubernetes,
// GitHub Actions, docker-compose, … auto-detected by filename), hover and
// diagnostics.
//
// Indent behavior (evaluated against internal/editor smart indent, stream
// 0260): IndentAfter suffixes are exactly the positions where YAML *requires*
// or conventionally continues one level deeper — a line ending in ":" opens a
// nested block mapping, and block-scalar introducers ("|", ">" and their
// chomping variants) require indented continuation lines. Everything else
// falls back to copy-indent, so sibling keys stay at their level and the
// editor never invents indentation YAML would reject.
//
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langyaml

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/highlights.scm
var query string

func init() {
	register.Language(lang.Language{
		ID:         "yaml",
		Extensions: []string{"yaml", "yml"},
		Grammar:    grammar(),
		Server: &lang.ServerSpec{
			Language:    "yaml",
			Command:     "yaml-language-server",
			Args:        []string{"--stdio"},
			RootMarkers: []string{".git"},
			Install:     []string{"npm", "install", "-g", "yaml-language-server"},
		},
		LineComment: "#",
		IndentAfter: []string{":", "|", "|-", "|+", ">", ">-", ">+"},
		// Sticky scopes + folding (#168, #144): a multi-line mapping pair pins
		// its key line (nested k8s/CI paths stay visible while scrolling).
		ScopeNodes: []string{"block_mapping_pair"},
		FoldNodes:  []string{"block_mapping_pair", "block_sequence", "block_scalar", "flow_mapping", "flow_sequence"},
	})
}

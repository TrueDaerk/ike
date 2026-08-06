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

	"ike/internal/cronhint"
	"ike/internal/escapes"
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
		// Base64 decoding (#1620): values in a Kubernetes Secret's data:
		// block conceal as the decoded text when the payload is printable.
		// Cron hints (#1624): a `cron:`/`schedule:` value — the CI workflow
		// case — and any quoted scalar of cron shape carry their English
		// reading after the expression.
		Spans: yamlSpans,
		// Shell in CI `run:` blocks (#1625): step scripts highlight with the
		// shell grammar; see regions.go for the gate and extent rules.
		Regions: shellRegions,
	})
}

// yamlSpans is the lang.Language.Spans hook: base64 Secret values decode
// (#1620) and cron expressions gain their schedule hint (#1624).
func yamlSpans(lines []string) []lang.Span {
	return append(escapes.Base64YAMLSpans(lines), cronhint.YAMLSpans(lines)...)
}

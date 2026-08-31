// Package langtoml registers TOML (#895): Tree-sitter highlighting via the
// tree-sitter-grammars/tree-sitter-toml grammar and the taplo language server
// for completion — schema-store aware (Cargo.toml, pyproject.toml, … detected
// by filename), plus formatting and diagnostics. Directly relevant to IKE
// itself: the IKE config is TOML. Self-registers via init(); blank-imported in
// cmd/ike/main.go.
package langtoml

import (
	_ "embed"

	"ike/internal/cronhint"
	"ike/internal/epochtime"
	"ike/internal/escapes"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/numhint"
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
		// Cron hints (#1624): a quoted cron expression — a scheduler entry in
		// a tool's TOML config — carries its English reading. Number hints
		// (#1627): byte sizes, durations, digit grouping and radix readings.
		Spans: tomlSpans,
	})
}

// tomlSpans is the lang.Language.Spans hook: quoted cron expressions gain
// their schedule hint (#1624), Unix epoch values their decoded UTC form
// (#1684) and numeric literals their readability hints (#1627).
func tomlSpans(lines []string) []lang.Span {
	// The epochs take their columns out of the number hints, as in the JSON
	// producer: two stand-ins over one literal would fight for the same cells.
	// A key that names the unit wins the other way round (#1685).
	hints, stamps := numhint.SpansWith(lines, epochtime.Spans(lines, epochtime.Value))
	out := append(cronhint.QuotedSpans(lines), escapes.UnicodeSpansIn(lines, escapes.UnicodeTOML)...)
	out = append(out, stamps...)
	out = append(out, hints...)
	return append(out, nethint.Spans(lines)...)
}

// Package langjson registers JSON and ndjson (#878): Tree-sitter highlighting
// via the official tree-sitter-json grammar (whose document rule is
// repeat(_value), so the same grammar parses both a single .json document and
// an ndjson stream), plus vscode-json-language-server for completion — the
// extracted VS Code server, whose JSON-Schema-store integration gives $schema
// and well-known-filename completion for free. It ships in the same npm
// package (vscode-langservers-extracted) already used for HTML/CSS (#855).
//
// ndjson/jsonl registers as its own language id sharing the grammar but
// deliberately **without** a server: the JSON server treats multiple top-level
// values as an error and would flag every line of a stream.
//
// Comment syntax stays empty even though the jsonc extension maps here: strict
// JSON has no comments, and advertising "//" would let comment-toggle corrupt
// plain .json files — the far more common case. JSONC buffers still highlight
// (the grammar has a comment node); only the toggle is unavailable.
//
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langjson

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
	g := grammar()
	register.Language(lang.Language{
		ID:         "json",
		Extensions: []string{"json", "jsonc"},
		Grammar:    g,
		Server: &lang.ServerSpec{
			Language:    "json",
			Command:     "vscode-json-language-server",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"package.json", ".git"},
			Install:     []string{"npm", "install", "-g", "vscode-langservers-extracted"},
			// The server only advertises documentFormattingProvider when
			// initializationOptions carries provideFormatter: true (#1505);
			// without it, Reformat File has no provider for JSON at all.
			Settings: map[string]any{"provideFormatter": true},
		},
		IndentAfter: []string{"{", "["},
		// Typing assistance (#1326): a JSON member reads "key": value, so the
		// space after the colon is written for you as you type.
		SpaceAfter: []rune{':'},
		// Foldable regions (#144): multi-line objects and arrays.
		FoldNodes: []string{"object", "array"},
		// Inline epoch decoding (#1618) and unicode-escape decoding (#1620):
		// a numeric timestamp in a value position conceals as its UTC form,
		// a \uXXXX escape in a string as the escaped character.
		Spans: jsonSpans,
	})

	register.Language(lang.Language{
		ID:         "ndjson",
		Extensions: []string{"ndjson", "jsonl"},
		Grammar:    g,
		FoldNodes:  []string{"object", "array"},
		SpaceAfter: []rune{':'},
		Spans:      jsonSpans,
	})
}

// jsonSpans is the lang.Language.Spans hook: Unix epoch timestamps in JSON
// value positions (#1618) and \uXXXX escapes in strings (#1620) become
// conceal-with-stand-in spans rendering the decoded form, a quoted cron
// expression gains its schedule hint (#1624), and numeric literals gain their
// readability hints (#1627) — byte sizes, durations, digit grouping, radix.
// The JSON context keeps keys and digits inside prose strings out of the epoch
// detection.
//
// The values of secret-suspect keys mask (#1813). Their spans come first:
// overlapping spans resolve first-covering-wins, so the mask must precede any
// decode that would otherwise render a piece of the credential.
func jsonSpans(lines []string) []lang.Span {
	// The number hints step aside where a timestamp already claimed the digits,
	// and win where the member name names the unit itself (#1685).
	hints, stamps := numhint.SpansWith(lines, epochtime.Spans(lines, epochtime.JSONValue))
	out := append(maskSpans(lines), stamps...)
	out = append(out, escapes.UnicodeSpansIn(lines, escapes.UnicodeJSON)...)
	out = append(out, cronhint.QuotedSpans(lines)...)
	// Network literals (#1653): a CIDR prefix or a punycode host in a value.
	out = append(out, nethint.Spans(lines)...)
	// Two stand-ins over one literal would fight for the same cells, so only
	// the surviving side of that resolution is emitted.
	return append(out, hints...)
}

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

	"ike/internal/lang"
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
		},
		IndentAfter: []string{"{", "["},
		// Foldable regions (#144): multi-line objects and arrays.
		FoldNodes: []string{"object", "array"},
	})

	register.Language(lang.Language{
		ID:         "ndjson",
		Extensions: []string{"ndjson", "jsonl"},
		Grammar:    g,
		FoldNodes:  []string{"object", "array"},
	})
}

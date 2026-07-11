// Package langgo registers the Go language: Tree-sitter highlighting plus the
// gopls language server. It self-registers via init() and is wired into the build
// by a blank import in cmd/ike/main.go. Adding a language to IKE means adding a
// package like this one — no engine edits.
package langgo

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/go.scm
var query string

func init() {
	register.Language(lang.Language{
		ID:         "go",
		Extensions: []string{"go"},
		Grammar:    grammar(),
		Server: &lang.ServerSpec{
			Language:    "go",
			Command:     "gopls",
			RootMarkers: []string{"go.mod", "go.work", ".git"},
			Install:     []string{"go", "install", "golang.org/x/tools/gopls@latest"},
			// gopls ships every inlay-hint kind off; enable the ones matching
			// what IKE renders (#171): parameter names and inferred types.
			// User config ([lsp.servers.go] settings) still overrides these,
			// and the lsp.inlay_hints toggle hides them client-side.
			Settings: map[string]any{
				"hints": map[string]any{
					"parameterNames":         true,
					"assignVariableTypes":    true,
					"rangeVariableTypes":     true,
					"compositeLiteralFields": true,
				},
			},
		},
		LineComment:  "//",
		BlockComment: [2]string{"/*", "*/"},
		IndentAfter:  []string{"{", "(", "["},
		// Sticky-scroll scopes (#168): declarations whose header line stays
		// pinned while scrolling through the body.
		ScopeNodes: []string{"function_declaration", "method_declaration", "func_literal", "type_declaration"},
		// New .go files start with their package clause, named after the
		// directory (#170). Override via `[lang.go] template`.
		Template: "package ${PACKAGE}\n",
	})
}

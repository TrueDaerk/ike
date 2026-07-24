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

//go:embed queries/injections.scm
var injections string

//go:embed queries/gomod.scm
var gomodQuery string

//go:embed queries/gowork.scm
var goworkQuery string

// tabs is the per-language indent default (#1137): gofmt output is
// tab-indented, so Go buffers (and the gofmt-formatted module metadata files)
// indent with tabs regardless of editor.use_spaces (.editorconfig still
// overrides). Declared as a variable because Language.UseTabs is a *bool
// (nil = no language opinion).
var tabs = true

func init() {
	register.Language(lang.Language{
		ID:         "go",
		Extensions: []string{"go"},
		Grammar:    grammar(),
		Toolchain:  toolchain{},
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
		UseTabs:      &tabs,
		// Sticky-scroll scopes (#168): declarations whose header line stays
		// pinned while scrolling through the body.
		ScopeNodes: []string{"function_declaration", "method_declaration", "func_literal", "type_declaration"},
		// Foldable regions (#144): declarations, blocks, import/const/var
		// groups, composite literals and multi-line /* */ comments.
		FoldNodes: []string{
			"function_declaration", "method_declaration", "func_literal",
			"type_declaration", "struct_type", "interface_type", "block",
			"import_declaration", "const_declaration", "var_declaration",
			"literal_value", "comment",
		},
		// New .go files start with their package clause, named after the
		// directory (#170). Override via `[lang.go] template`.
		Template: "package ${PACKAGE}\n",
	})

	// Module metadata files (#1063): matched by exact base name, delegated to
	// the "go" server — gopls fully supports them under the wire languageIds
	// "go.mod" / "go.work" / "go.sum" (each language's own ID), attaching to
	// the same instance/root as the module's .go files. Registered with plain
	// lang.Register, not register.Language: they are part of the go plugin
	// (its lang-go toggle governs the shared server), and a dotted plugin id
	// would splinter the plugins.<id>.enabled config key. go.mod is
	// highlighted by the vendored tree-sitter-go-mod grammar (#1078); go.work
	// by the dedicated vendored tree-sitter-go-work grammar (#1119), whose
	// `use` directive the gomod grammar lacks — see grammar_gowork_cgo.go for
	// the vendoring notes and the `toolchain` trade-off. go.sum stays plain —
	// no grammar exists, content is hashes.
	lang.Register(lang.Language{
		ID:             "go.mod",
		Filenames:      []string{"go.mod"},
		ServerLanguage: "go",
		Grammar:        gomodGrammar(),
		LineComment:    "//",
		IndentAfter:    []string{"("},
		UseTabs:        &tabs,
	})
	lang.Register(lang.Language{
		ID:             "go.work",
		Filenames:      []string{"go.work"},
		ServerLanguage: "go",
		Grammar:        goworkGrammar(),
		LineComment:    "//",
		IndentAfter:    []string{"("},
		UseTabs:        &tabs,
	})
	lang.Register(lang.Language{
		ID:             "go.sum",
		Filenames:      []string{"go.sum"},
		ServerLanguage: "go",
	})
}

// Package langphp registers PHP: Tree-sitter highlighting plus the intelephense
// language server. Self-registers via init(); blank-imported in cmd/ike/main.go.
package langphp

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/php.scm
var query string

func init() {
	register.Language(lang.Language{
		ID:         "php",
		Extensions: []string{"php", "phtml"},
		// Shebang fallback (#893): extensionless CLI scripts.
		Interpreters: []string{"php"},
		Grammar:    grammar(),
		Server: &lang.ServerSpec{
			Language:    "php",
			Command:     "intelephense",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"composer.json", ".git"},
			Install:     []string{"npm", "install", "-g", "intelephense"},
		},
		Toolchain:    toolchain{},
		LineComment:  "//",
		BlockComment: [2]string{"/*", "*/"},
		IndentAfter:  []string{"{", "(", "["},
		// Sticky-scroll scopes (#168).
		ScopeNodes: []string{
			"function_definition", "method_declaration", "anonymous_function",
			"class_declaration", "interface_declaration", "trait_declaration",
			"enum_declaration", "namespace_definition",
		},
		// Foldable regions (#144): declarations, statement blocks, array
		// literals and multi-line /* */ comments.
		FoldNodes: []string{
			"function_definition", "method_declaration", "anonymous_function",
			"class_declaration", "interface_declaration", "trait_declaration",
			"enum_declaration", "namespace_definition", "compound_statement",
			"declaration_list", "array_creation_expression", "comment",
		},
		// New .php files start with the opening tag (#170). Override via
		// `[lang.php] template`.
		Template: "<?php\n\n",
	})
}

// Package langphp registers PHP: Tree-sitter highlighting plus the intelephense
// language server. Self-registers via init(); blank-imported in cmd/ike/main.go.
package langphp

import (
	_ "embed"

	"ike/internal/consthint"
	"ike/internal/escapes"
	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/php.scm
var query string

//go:embed queries/injections.scm
var injections string

func init() {
	register.Language(lang.Language{
		ID:         "php",
		Extensions: []string{"php", "phtml"},
		// Shebang fallback (#893): extensionless CLI scripts.
		Interpreters: []string{"php"},
		Grammar:      grammar(),
		// The Composer dependency manifest (#2419): the Dependencies tool
		// window scans it via composer outdated + audit.
		DepManifests: []string{"composer.json"},
		Server: &lang.ServerSpec{
			Language:    "php",
			Command:     "intelephense",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"composer.json", ".git"},
			Install:     []string{"npm", "install", "-g", "intelephense"},
		},
		Toolchain: toolchain{},
		// Constant conceals (#1701): `const` and `define()` declarations get
		// unit readings derived from the constant's name, with pure literal
		// arithmetic evaluated first.
		Spans:        phpSpans,
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
		// Test runner (#1926): PHPUnit — `▶` gutter markers on the test
		// methods of `*Test.php`, and a structured Test Results tree fed by
		// the `--teamcity` service-message stream. See test.go / testoutput.go.
		Test: phpunitSpec,
	})
}

// phpSpans is the lang.Language.Spans hook: the constant conceals (#1701) and
// the unicode-escape stand-ins (#1620, #2334). Only double-quoted strings
// decode — PHP's '…' passes a backslash through unchanged.
func phpSpans(lines []string) []lang.Span {
	return append(consthint.PHPSpans(lines), escapes.UnicodeSpansIn(lines, escapes.UnicodePHP)...)
}

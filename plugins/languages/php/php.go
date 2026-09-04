// Package langphp registers PHP: Tree-sitter highlighting plus the intelephense
// language server. Self-registers via init(); blank-imported in cmd/ike/main.go.
package langphp

import (
	_ "embed"

	"ike/internal/consthint"
	"ike/internal/cronhint"
	"ike/internal/escapes"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/permhint"
	"ike/internal/secret"
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

// phpSpans is the lang.Language.Spans hook: the secret masks on suspect
// assignments (#2345), the constant conceals (#1701), the unicode-escape
// stand-ins (#1620, #2334 — only double-quoted strings decode, PHP's '…'
// passes a backslash through unchanged), the network-literal hints (#1653)
// and cron hints (#2345) in string literals, the permission hints on
// chmod()/mkdir() calls (#2345), and the entity decoding in the buffer's
// HTML portions (#2345). The masks come first: overlapping spans resolve
// first-covering-wins, so the mask must precede any decode that would render
// a piece of the credential.
func phpSpans(lines []string) []lang.Span {
	out := append(secret.AssignSpans(lines), consthint.PHPSpans(lines)...)
	out = append(out, escapes.UnicodeSpansIn(lines, escapes.UnicodePHP)...)
	out = append(out, nethint.QuotedSpans(lines)...)
	out = append(out, permhint.PHPSpans(lines)...)
	out = append(out, cronhint.QuotedSpans(lines)...)
	return append(out, entitySpans(lines)...)
}

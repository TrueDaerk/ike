// Package langweb registers the web languages (Roadmap 0410, #855):
// TypeScript/JavaScript, HTML and CSS, each with its evaluated default
// language server, and — since #925 — Tree-sitter highlighting: one TSX
// grammar for every JS/TS dialect (see grammar_cgo.go for why), the official
// HTML grammar with <script>/<style> injections, and the official CSS
// grammar (scss/less parse best-effort under it — error-tolerant spans still
// color the shared subset).
//
// Server choices (#855):
//   - TS/JS → vtsls: wraps the same tsserver VS Code uses, but implements the
//     LSP completion/limits/isIncomplete model much more faithfully than
//     typescript-language-server (better streaming completions, lower memory
//     churn on big projects). Override via [lsp.servers.typescript].
//   - HTML/CSS → vscode-langservers-extracted: the extracted VS Code
//     html/css servers are the de-facto standard; nothing else matches their
//     attribute/property data. One npm package ships both binaries.
//   - PHP stays on Intelephense (registered in plugins/languages/php): free
//     tier beats phpactor on completion quality and speed; premium features
//     (rename across files, advanced refactors) are a paid license — swap to
//     phpactor via [lsp.servers.php] if that matters more than completion.
//
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langweb

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/typescript.scm
var tsQuery string

//go:embed queries/html.scm
var htmlQuery string

//go:embed queries/html_injections.scm
var htmlInjections string

//go:embed queries/css.scm
var cssQuery string

func init() {
	register.Language(lang.Language{
		ID:         "typescript",
		Extensions: []string{"ts", "tsx", "js", "jsx", "mjs", "cjs", "mts", "cts"},
		Grammar:    tsGrammar(),
		Server: &lang.ServerSpec{
			Language:    "typescript",
			Command:     "vtsls",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"tsconfig.json", "jsconfig.json", "package.json", ".git"},
			Install:     []string{"npm", "install", "-g", "@vtsls/language-server"},
		},
		LineComment:  "//",
		BlockComment: [2]string{"/*", "*/"},
		IndentAfter:  []string{"{", "(", "["},
		// Sticky scopes + folding (#168, #144).
		ScopeNodes: []string{"function_declaration", "method_definition", "class_declaration", "arrow_function", "function_expression"},
		FoldNodes: []string{
			"function_declaration", "method_definition", "class_declaration",
			"arrow_function", "function_expression", "statement_block",
			"object", "array", "interface_declaration", "enum_declaration",
			"switch_statement", "jsx_element", "comment",
		},
	})

	register.Language(lang.Language{
		ID:         "html",
		Extensions: []string{"html", "htm", "xhtml"},
		Grammar:    htmlGrammar(),
		Server: &lang.ServerSpec{
			Language:    "html",
			Command:     "vscode-html-language-server",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"package.json", ".git"},
			Install:     []string{"npm", "install", "-g", "vscode-langservers-extracted"},
		},
		BlockComment: [2]string{"<!--", "-->"},
		IndentAfter:  []string{">"},
		FoldNodes:    []string{"element", "script_element", "style_element", "comment"},
	})

	register.Language(lang.Language{
		ID:         "css",
		Extensions: []string{"css", "scss", "less"},
		Grammar:    cssGrammar(),
		Server: &lang.ServerSpec{
			Language:    "css",
			Command:     "vscode-css-language-server",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"package.json", ".git"},
			Install:     []string{"npm", "install", "-g", "vscode-langservers-extracted"},
		},
		BlockComment: [2]string{"/*", "*/"},
		IndentAfter:  []string{"{"},
		// Sticky scopes + folding: rule headers pin, blocks fold.
		ScopeNodes: []string{"rule_set", "media_statement", "keyframes_statement", "supports_statement"},
		FoldNodes:  []string{"rule_set", "media_statement", "keyframes_statement", "supports_statement", "block", "comment"},
	})
}

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

	"ike/internal/consthint"
	"ike/internal/cronhint"
	"ike/internal/escapes"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/permhint"
	"ike/internal/secret"
	"ike/plugins/languages/register"
)

// htmlEntitySpans is the HTML lang.Language.Spans hook (#1620): character
// references conceal as the decoded text, by the full HTML entity table.
func htmlEntitySpans(lines []string) []lang.Span {
	return escapes.EntitySpans(lines, escapes.EntityHTML)
}

//go:embed queries/typescript.scm
var tsQuery string

//go:embed queries/ts_injections.scm
var tsInjections string

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
		// The npm dependency manifest (#2419): the Dependencies tool window
		// scans it via npm/pnpm/yarn outdated + audit.
		DepManifests: []string{"package.json"},
		Server: &lang.ServerSpec{
			Language:    "typescript",
			Command:     "vtsls",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"tsconfig.json", "jsconfig.json", "package.json", ".git"},
			Install:     []string{"npm", "install", "-g", "@vtsls/language-server"},
			// Embedded <script> shadow documents (#2330): vtsls (the VS Code
			// TypeScript extension behind an LSP facade) silently ignores
			// documents whose URI scheme is not on the extension's supported
			// list — untitled is, and untitled documents join an inferred
			// tsserver project with the default libs, so document.<members>
			// complete.
			FragmentScheme: "untitled",
		},
		// Workspace-TypeScript detection (#1079): vendored TS wins.
		Toolchain:    tsToolchain{},
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
		// Unicode-escape decoding (#1620): \uXXXX (surrogate pairs combined)
		// in string literals conceals as the escaped character. Network
		// literals (#1653): a CIDR prefix or punycode host inside a string
		// literal carries its reading.
		Spans: scriptSpans,
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
			// The server only advertises documentFormattingProvider when
			// initializationOptions carries provideFormatter: true (#1507);
			// without it, Reformat File has no provider for HTML at all.
			Settings: map[string]any{"provideFormatter": true},
		},
		BlockComment: [2]string{"<!--", "-->"},
		IndentAfter:  []string{">"},
		FoldNodes:    []string{"element", "script_element", "style_element", "comment"},
		// Embedded-language LSP (#2330): the <script>/<style> fragments the
		// injection query detects merge into one blanked shadow document per
		// embedded language (typescript → vtsls, css → the css server), so
		// completion/hover/diagnostics work inside them and all script tags
		// share one scope. Inline on* attribute handlers are not injection
		// captures and stay without LSP delegation — expression context,
		// little value.
		EmbeddedShadow: true,
		// Entity decoding (#1620): &amp;, &#x2026; and friends conceal as the
		// decoded character — the full HTML named-entity table applies.
		Spans: htmlEntitySpans,
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
			// Same mechanism as HTML/JSON (#1507): formatting is only
			// advertised when provideFormatter: true is passed at initialize.
			Settings: map[string]any{"provideFormatter": true},
		},
		BlockComment: [2]string{"/*", "*/"},
		IndentAfter:  []string{"{"},
		// Sticky scopes + folding: rule headers pin, blocks fold.
		ScopeNodes: []string{"rule_set", "media_statement", "keyframes_statement", "supports_statement"},
		FoldNodes:  []string{"rule_set", "media_statement", "keyframes_statement", "supports_statement", "block", "comment"},
		// Unicode-escape decoding (#2345): CSS's `\e9` / `\00e9` character
		// escapes conceal as the character they name.
		Spans: cssSpans,
	})
}

// scriptSpans is the JavaScript/TypeScript lang.Language.Spans hook: the
// secret masks on suspect assignments (#2345), the unicode-escape stand-ins
// (#1620) — including the ES6 \u{X…} form and \xNN, and inside `…` templates
// (#2334) — the network-literal hints (#1653) and cron hints (#2345)
// restricted to string literals (a bare `10.0.0.0/8` in source is
// arithmetic, not a prefix), the permission hints on the fs mode APIs
// (#2345), the constant conceals on CONST_CASE assignments (#2345) and the
// entity decoding in JSX text (#2345). The masks come first: overlapping
// spans resolve first-covering-wins, so the mask must precede any decode
// that would render a piece of the credential.
func scriptSpans(lines []string) []lang.Span {
	out := append(secret.AssignSpans(lines), escapes.UnicodeSpansIn(lines, escapes.UnicodeScript)...)
	out = append(out, nethint.QuotedSpans(lines)...)
	out = append(out, permhint.ScriptSpans(lines)...)
	out = append(out, cronhint.QuotedSpans(lines)...)
	out = append(out, consthint.ScriptSpans(lines)...)
	return append(out, jsxEntitySpans(lines)...)
}

// cssSpans is the CSS lang.Language.Spans hook (#2345): character escapes —
// `\e9`, `content: "\f00c"` — conceal as the character they name, CSS's own
// no-prefix hex dialect.
func cssSpans(lines []string) []lang.Span {
	return escapes.UnicodeCSSSpans(lines)
}

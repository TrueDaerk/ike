// Package langweb registers the web languages (Roadmap 0410, #855):
// TypeScript/JavaScript, HTML and CSS, each with its evaluated default
// language server. No Tree-sitter grammars ship here yet — completion,
// diagnostics and navigation come from the servers (plus the local word/
// symbol indexes); highlighting for these languages is a separate work item.
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
	"ike/internal/lang"
	"ike/plugins/languages/register"
)

func init() {
	register.Language(lang.Language{
		ID:         "typescript",
		Extensions: []string{"ts", "tsx", "js", "jsx", "mjs", "cjs", "mts", "cts"},
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
	})

	register.Language(lang.Language{
		ID:         "html",
		Extensions: []string{"html", "htm", "xhtml"},
		Server: &lang.ServerSpec{
			Language:    "html",
			Command:     "vscode-html-language-server",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"package.json", ".git"},
			Install:     []string{"npm", "install", "-g", "vscode-langservers-extracted"},
		},
		BlockComment: [2]string{"<!--", "-->"},
		IndentAfter:  []string{">"},
	})

	register.Language(lang.Language{
		ID:         "css",
		Extensions: []string{"css", "scss", "less"},
		Server: &lang.ServerSpec{
			Language:    "css",
			Command:     "vscode-css-language-server",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"package.json", ".git"},
			Install:     []string{"npm", "install", "-g", "vscode-langservers-extracted"},
		},
		BlockComment: [2]string{"/*", "*/"},
		IndentAfter:  []string{"{"},
	})
}

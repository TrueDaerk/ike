// Package langsql registers SQL: Tree-sitter highlighting (for .sql files and
// for SQL fragments injected into other languages, issue #299), and the
// sqls language server for .sql files and — via the embedded-fragment seam
// (roadmap 0300) — for SQL strings inside other languages.
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langsql

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/highlights.scm
var query string

func init() {
	register.Language(lang.Language{
		ID:         "sql",
		Extensions: []string{"sql"},
		Grammar:    grammar(),
		Server: &lang.ServerSpec{
			Language: "sql",
			// sqls (github.com/sqls-server/sqls): maintained Go binary,
			// LSP over stdio by default, no flags needed. Replaced
			// sql-language-server, which crashes under Node >= 26 (#1066).
			// Database connections are optional (.sqls/config.yml or
			// ~/.config/sqls/config.yml); without one it still provides
			// keyword/function completion and formatting.
			Command:     "sqls",
			RootMarkers: []string{".sqls", ".git"},
			Install:     []string{"go", "install", "github.com/sqls-server/sqls@latest"},
		},
		LineComment:  "--",
		BlockComment: [2]string{"/*", "*/"},
		// Foldable regions (#144): multi-line statements and /* */ comments.
		FoldNodes: []string{"statement", "comment"},
	})
}

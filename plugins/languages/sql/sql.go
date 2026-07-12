// Package langsql registers SQL: Tree-sitter highlighting (for .sql files and
// for SQL fragments injected into other languages, issue #299), and the
// sql-language-server for .sql files and — via the embedded-fragment seam
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
			Language:    "sql",
			Command:     "sql-language-server",
			Args:        []string{"up", "--method", "stdio"},
			RootMarkers: []string{".sqllsrc.json", ".git"},
			Install:     []string{"npm", "install", "-g", "sql-language-server"},
		},
		LineComment:  "--",
		BlockComment: [2]string{"/*", "*/"},
		// Foldable regions (#144): multi-line statements and /* */ comments.
		FoldNodes: []string{"statement", "comment"},
	})
}

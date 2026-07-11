// Package langsql registers SQL: the sql-language-server for .sql files and —
// via the embedded-fragment seam (roadmap 0300) — for SQL strings inside other
// languages. No Tree-sitter grammar yet, so .sql files have no highlighting;
// fragment detection runs on the host language's grammar and does not need one.
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langsql

import (
	"ike/internal/lang"
	"ike/plugins/languages/register"
)

func init() {
	register.Language(lang.Language{
		ID:         "sql",
		Extensions: []string{"sql"},
		Server: &lang.ServerSpec{
			Language:    "sql",
			Command:     "sql-language-server",
			Args:        []string{"up", "--method", "stdio"},
			RootMarkers: []string{".sqllsrc.json", ".git"},
			Install:     []string{"npm", "install", "-g", "sql-language-server"},
		},
		LineComment:  "--",
		BlockComment: [2]string{"/*", "*/"},
	})
}

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

func init() {
	register.Language(lang.Language{
		ID:         "go",
		Extensions: []string{"go"},
		Grammar:    grammar(),
		Server: &lang.ServerSpec{
			Language:    "go",
			Command:     "gopls",
			RootMarkers: []string{"go.mod", "go.work", ".git"},
			Install:     []string{"go", "install", "golang.org/x/tools/gopls@latest"},
		},
		LineComment:  "//",
		BlockComment: [2]string{"/*", "*/"},
	})
}

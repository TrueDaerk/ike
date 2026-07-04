// Package langgo registers the Go language: Tree-sitter highlighting plus the
// gopls language server. It self-registers via init() and is wired into the build
// by a blank import in cmd/ike/main.go. Adding a language to IKE means adding a
// package like this one — no engine edits.
package langgo

import (
	_ "embed"

	"ike/internal/lang"
)

//go:embed queries/go.scm
var query string

func init() {
	lang.Register(lang.Language{
		ID:         "go",
		Extensions: []string{"go"},
		Grammar:    grammar(),
		Server: &lang.ServerSpec{
			Language:    "go",
			Command:     "gopls",
			RootMarkers: []string{"go.mod", "go.work", ".git"},
		},
	})
}

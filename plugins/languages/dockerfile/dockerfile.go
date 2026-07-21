// Package langdockerfile registers Dockerfile (#896): Tree-sitter highlighting
// via the camdencheek/tree-sitter-dockerfile grammar (vendored C source, see
// grammar_cgo.go) and docker-langserver from dockerfile-language-server-nodejs
// for completion — instructions, flags, image names — plus diagnostics for
// common mistakes. Matches by exact base name (Dockerfile, Containerfile) and
// by the .dockerfile extension (api.dockerfile style). Self-registers via
// init(); blank-imported in cmd/ike/main.go.
package langdockerfile

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/highlights.scm
var query string

func init() {
	register.Language(lang.Language{
		ID:         "dockerfile",
		Extensions: []string{"dockerfile"},
		Filenames:  []string{"Dockerfile", "Containerfile"},
		Grammar:    grammar(),
		Server: &lang.ServerSpec{
			Language:    "dockerfile",
			Command:     "docker-langserver",
			Args:        []string{"--stdio"},
			RootMarkers: []string{".git"},
			Install:     []string{"npm", "install", "-g", "dockerfile-language-server-nodejs"},
		},
		LineComment: "#",
	})
}

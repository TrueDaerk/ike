// Package langphp registers PHP: Tree-sitter highlighting plus the intelephense
// language server. Self-registers via init(); blank-imported in cmd/ike/main.go.
package langphp

import (
	_ "embed"

	"ike/internal/lang"
)

//go:embed queries/php.scm
var query string

func init() {
	lang.Register(lang.Language{
		ID:         "php",
		Extensions: []string{"php", "phtml"},
		Grammar:    grammar(),
		Server: &lang.ServerSpec{
			Language:    "php",
			Command:     "intelephense",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"composer.json", ".git"},
			Install:     []string{"npm", "install", "-g", "intelephense"},
		},
		Toolchain:    toolchain{},
		LineComment:  "//",
		BlockComment: [2]string{"/*", "*/"},
	})
}

// Package langpython registers Python: Tree-sitter highlighting, the pyright
// language server, and a toolchain detector that resolves the project interpreter
// (active venv / .venv / .python-version / pyproject) and hands its path to
// pyright so version-aware diagnostics check against the project's actual
// interpreter. Self-registers via init(); blank-imported in cmd/ike/main.go.
package langpython

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/python.scm
var query string

func init() {
	register.Language(lang.Language{
		ID:         "python",
		Extensions: []string{"py", "pyi"},
		Grammar:    grammar(),
		Server: &lang.ServerSpec{
			Language:    "python",
			Command:     "pyright-langserver",
			Args:        []string{"--stdio"},
			RootMarkers: []string{"pyproject.toml", "setup.py", "setup.cfg", ".git"},
			Install:     []string{"npm", "install", "-g", "pyright"},
		},
		Toolchain:   toolchain{},
		LineComment: "#",
		IndentAfter: []string{":", "(", "[", "{"},
	})
}

// Package langshell registers Shell (#894): Tree-sitter highlighting via the
// official tree-sitter-bash grammar (covers sh/zsh well enough for
// highlighting) and bash-language-server for completion — commands from PATH,
// variables, function names — which also surfaces shellcheck diagnostics
// automatically when shellcheck is on PATH. Matches by extension, by the
// common rc-file base names, and — via the shebang fallback (#893) — by
// interpreter for extensionless scripts. Self-registers via init();
// blank-imported in cmd/ike/main.go.
package langshell

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/highlights.scm
var query string

func init() {
	register.Language(lang.Language{
		ID:         "shell",
		Extensions: []string{"sh", "bash", "zsh"},
		Filenames:  []string{".bashrc", ".zshrc", ".bash_profile", ".profile", ".zprofile"},
		// Shebang fallback (#893): extensionless scripts.
		Interpreters: []string{"sh", "bash", "zsh", "dash"},
		Grammar:      grammar(),
		Server: &lang.ServerSpec{
			Language:    "shell",
			Command:     "bash-language-server",
			Args:        []string{"start"},
			RootMarkers: []string{".git"},
			Install:     []string{"npm", "install", "-g", "bash-language-server"},
		},
		LineComment: "#",
		IndentAfter: []string{"then", "do", "{"},
		// Sticky scopes + folding (#168, #144).
		ScopeNodes: []string{"function_definition", "if_statement", "for_statement", "while_statement", "case_statement"},
		FoldNodes: []string{
			"function_definition", "if_statement", "for_statement",
			"while_statement", "case_statement", "compound_statement",
			"do_group", "heredoc_body",
		},
	})
}

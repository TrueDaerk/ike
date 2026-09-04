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

	"ike/internal/escapes"
	"ike/internal/lang"
	"ike/internal/permhint"
	"ike/plugins/languages/register"
)

// shellSpans is the lang.Language.Spans hook: the secret masks (#2345) come
// first — overlapping spans resolve first-covering-wins, so the mask must
// precede anything that would render a piece of the credential — then the
// permission hints (#1656) and the unicode escapes of ANSI-C quoting
// ($'café', #2345).
func shellSpans(lines []string) []lang.Span {
	out := append(maskSpans(lines), permhint.ShellSpans(lines)...)
	return append(out, escapes.UnicodeANSICSpans(lines)...)
}

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
		// Run-command seam only (#1225); no server settings to detect.
		Toolchain: toolchain{},
		Server: &lang.ServerSpec{
			Language:    "shell",
			Command:     "bash-language-server",
			Args:        []string{"start"},
			RootMarkers: []string{".git"},
			Install:     []string{"npm", "install", "-g", "bash-language-server"},
			// bash-language-server delegates linting to shellcheck; without
			// it on PATH the server runs but never publishes a diagnostic
			// (#1067) — the manager surfaces a one-time hint.
			Companions: []lang.Companion{
				{Binary: "shellcheck", Purpose: "shell diagnostics", Install: "brew install shellcheck"},
			},
		},
		// Permission hints (#1656): `chmod`'s mode operand and the `-m` value
		// of `install`/`mkdir` draw their symbolic rwx form. Secret masking
		// and ANSI-C unicode decoding joined in #2345.
		Spans:       shellSpans,
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

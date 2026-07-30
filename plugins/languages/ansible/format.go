package langansible

import "ike/internal/format"

// format.go wires Ansible's default external formatters (Roadmap 0470,
// #1405): ansible-language-server advertises no formatting provider, and
// Ansible files are YAML — so reformat runs prettier's YAML parser when
// available, else yamlfmt, reusing the same tools a plain YAML buffer would.
// `[format.ansible]` overrides the chain (#1402).

func init() {
	format.RegisterExternalDefaults("ansible",
		format.External{
			Command: "prettier",
			Args:    []string{"--parser", "yaml", "--stdin-filepath", "${FILE}"},
			Install: "npm install -g prettier",
		},
		format.External{
			Command: "yamlfmt",
			Args:    []string{"-in"},
			Install: "go install github.com/google/yamlfmt/cmd/yamlfmt@latest",
		},
	)
}

package langyaml

import "ike/internal/format"

// format.go wires YAML's default external formatters (Roadmap 0470, #1405,
// #2596): yaml-language-server's own formatting is an opt-in wrapper around
// prettier, so reformat calls prettier's YAML parser directly when available,
// else yamlfmt — the same pair the Ansible plugin already uses for its YAML
// dialect, so a playbook and a plain .yml format alike. A project-local
// `node_modules/.bin/prettier` wins over the PATH install; biome has no YAML
// formatter, so it is not in the chain. `[format.yaml]` overrides it (#1402).

func init() {
	yamlPrettier := func(command string) format.External {
		return format.External{
			Command: command,
			Args:    []string{"--parser", "yaml", "--stdin-filepath", "${FILE}"},
			Install: "npm install -g prettier",
		}
	}
	format.RegisterExternalDefaults("yaml",
		yamlPrettier("node_modules/.bin/prettier"),
		yamlPrettier("prettier"),
		format.External{
			Command: "yamlfmt",
			Args:    []string{"-in"},
			Install: "go install github.com/google/yamlfmt/cmd/yamlfmt@latest",
		},
	)
}

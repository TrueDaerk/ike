package langjson

import "ike/internal/format"

// format.go wires JSON's default external formatters (Roadmap 0470, #1405,
// #2596): vscode-json-language-server only re-indents around the line breaks
// a buffer already has, so a minified .json stays minified. prettier (else
// biome, which covers JSON natively) pretty-prints it. Project-local
// installs win over PATH installs — `node_modules/.bin/…` resolves against
// the process working directory, which is the project root.
//
// `ndjson` deliberately keeps no default: one JSON document per line is the
// format's whole point, and prettier would expand each record across lines.
// `[format.json]` overrides the chain (#1402).

func init() {
	format.RegisterExternalDefaults("json",
		jsonPrettier("node_modules/.bin/prettier"),
		jsonBiome("node_modules/.bin/biome"),
		jsonPrettier("prettier"),
		jsonBiome("biome"),
	)
}

// jsonPrettier is the prettier spec: stdin mode with the real file name, so
// the parser and the project's .prettierrc follow from the path.
func jsonPrettier(command string) format.External {
	return format.External{
		Command: command,
		Args:    []string{"--stdin-filepath", "${FILE}"},
		Install: "npm install -g prettier",
	}
}

// jsonBiome is the biome spec: `biome format` reads stdin when told the file
// path it stands for.
func jsonBiome(command string) format.External {
	return format.External{
		Command: command,
		Args:    []string{"format", "--stdin-file-path=${FILE}"},
		Install: "npm install -g @biomejs/biome",
	}
}

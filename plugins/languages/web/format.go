package langweb

import "ike/internal/format"

// format.go wires the web languages' default external formatters (Roadmap
// 0470, #1405, #2596): the servers behind JS/TS, HTML and CSS only normalize
// whitespace around the line breaks a buffer already has — tsserver (via
// vtsls) never wraps, so Reformat File on a minified one-liner answers "no
// changes" and leaves it minified. Real pretty-printing is the ecosystem's
// job: prettier, else biome where it covers the language (JS/TS/CSS; its
// HTML formatter is still experimental, so HTML stays prettier-only).
//
// Project-local installs win over PATH installs — `node_modules/.bin/…`
// resolves against the process working directory, which is the project root,
// mirroring Python's venv-first chain. Both tools run with the project root
// as cwd and receive the real file name on stdin mode, so they pick up the
// project's .prettierrc / biome.json and infer the parser from the
// extension. Neither takes a *line* range (prettier's --range-start/--end
// are character offsets), so no RangeArgs is declared and Reformat Selection
// falls through to the LSP tier exactly as before (#1401).
// `[format.<lang>]` overrides the whole chain (#1402).

func init() {
	for _, langID := range []string{"typescript", "css"} {
		format.RegisterExternalDefaults(langID,
			prettier("node_modules/.bin/prettier"),
			biome("node_modules/.bin/biome"),
			prettier("prettier"),
			biome("biome"),
		)
	}
	format.RegisterExternalDefaults("html",
		prettier("node_modules/.bin/prettier"),
		prettier("prettier"),
	)
}

// prettier is the shared spec: stdin mode with the real file name, so the
// parser and the project config are inferred from the path.
func prettier(command string) format.External {
	return format.External{
		Command: command,
		Args:    []string{"--stdin-filepath", "${FILE}"},
		Install: "npm install -g prettier",
	}
}

// biome is the shared spec: `biome format` reads stdin when told the file
// path it stands for.
func biome(command string) format.External {
	return format.External{
		Command: command,
		Args:    []string{"format", "--stdin-file-path=${FILE}"},
		Install: "npm install -g @biomejs/biome",
	}
}

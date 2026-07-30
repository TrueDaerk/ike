package langmarkdown

import "ike/internal/format"

// format.go wires Markdown's default external formatters (Roadmap 0470,
// #1405): marksman advertises no formatting provider, so reformat runs
// prettier when available, else mdformat. Neither reflows prose paragraphs
// by default — prettier's proseWrap defaults to "preserve" and mdformat does
// not wrap — so no hard wrapping happens unless the user configures the tool
// to. `[format.markdown]` overrides the chain (#1402).

func init() {
	format.RegisterExternalDefaults("markdown",
		format.External{
			Command: "prettier",
			Args:    []string{"--parser", "markdown", "--stdin-filepath", "${FILE}"},
			Install: "npm install -g prettier",
		},
		format.External{
			Command: "mdformat",
			Args:    []string{"-"},
			Install: "pip install mdformat",
		},
	)
}

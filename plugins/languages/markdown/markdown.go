// Package langmarkdown registers Markdown (#880): Tree-sitter highlighting via
// the two-part tree-sitter-grammars/tree-sitter-markdown grammar — a block
// grammar for structure and a separate inline grammar for emphasis, code spans
// and links, wired together through the injection seam
// (internal/highlight/fragment.go): every (inline) block node is re-parsed
// with the inline grammar, and fenced code blocks inject the registered
// grammar named by their info string ("```go" renders as Go) via the dynamic
// @fragment.language / @fragment.content pair. YAML/TOML front matter injects
// those grammars too.
//
// Completion comes from marksman (a single static binary): link targets,
// heading anchors, wiki-links and reference labels; plain prose keeps the
// existing word-index source.
//
// Both grammars are vendored C source under grammar/ (see grammar_block_cgo.go)
// — upstream ships no Go bindings. Self-registers via init(); blank-imported
// in cmd/ike/main.go.
package langmarkdown

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/highlights.scm
var blockQuery string

//go:embed queries/injections.scm
var injectionsQuery string

//go:embed queries/highlights_inline.scm
var inlineQuery string

func init() {
	register.Language(lang.Language{
		ID:         "markdown",
		Extensions: []string{"md", "markdown"},
		Grammar:    blockGrammar(),
		Server: &lang.ServerSpec{
			Language:    "markdown",
			Command:     "marksman",
			Args:        []string{"server"},
			RootMarkers: []string{".marksman.toml", ".git"},
			Install:     []string{"brew", "install", "marksman"},
		},
		BlockComment: [2]string{"<!--", "-->"},
		// Sticky scopes + folding (#168, #144): a section spans its heading
		// plus content, so headings pin while their section scrolls.
		ScopeNodes: []string{"section"},
		FoldNodes:  []string{"section", "fenced_code_block", "list", "block_quote", "pipe_table"},
	})

	// The inline grammar registers as its own (extension-less, server-less)
	// language so the injection overlay can resolve @fragment.markdown_inline
	// through the ordinary registry lookup.
	register.Language(lang.Language{
		ID:      "markdown_inline",
		Grammar: inlineGrammar(),
	})
}

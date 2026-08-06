// Package langxml registers XML (#1253): Tree-sitter highlighting via the
// tree-sitter-grammars/tree-sitter-xml grammar (vendored C source, see
// grammar_cgo.go — the repo's `xml` grammar only; its sibling `dtd` grammar
// is not vendored). Matched by the .xml extension plus the common XML
// dialects that share the same syntax (.xsd, .xsl/.xslt, .svg, .plist,
// .csproj and friends). No LSP server: the mainstream XML servers (lemminx)
// need a JVM, so this is highlighting-only — the same call the Makefile and
// TOML plugins make. Self-registers via init(); blank-imported in
// cmd/ike/main.go.
package langxml

import (
	_ "embed"

	"ike/internal/escapes"
	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/highlights.scm
var query string

func init() {
	register.Language(lang.Language{
		ID: "xml",
		Extensions: []string{
			"xml",
			// XML dialects that parse under the same grammar.
			"xsd", "xsl", "xslt", "svg", "plist", "wsdl",
			"csproj", "vbproj", "fsproj", "props", "targets",
		},
		Grammar: grammar(),
		// XML has no line comment — only the <!-- --> block form (#1253).
		BlockComment: [2]string{"<!--", "-->"},
		IndentAfter:  []string{">"},
		// Sticky scopes + folding (#168, #144): the open tag pins while the
		// element body scrolls; elements, CDATA and comments collapse.
		ScopeNodes: []string{"element"},
		FoldNodes:  []string{"element", "CDSect", "Comment", "doctypedecl"},
		// Entity decoding (#1620): only XML's five predefined entities plus
		// numeric references — other names are document-defined, guessing the
		// HTML table for them would lie.
		Spans: entitySpans,
	})
}

// entitySpans is the lang.Language.Spans hook (#1620): character references
// conceal as the decoded text.
func entitySpans(lines []string) []lang.Span {
	return escapes.EntitySpans(lines, escapes.EntityXML)
}

// Package langhttp registers the .http request-file language (#1250, epic
// #1247): Tree-sitter highlighting via the rest-nvim/tree-sitter-http grammar
// (vendored C source, see grammar_cgo.go) for request lines, headers,
// comments, ### separators and placeholders, plus the Content-Type-driven
// body regions that make a JSON body JSON (regions.go). The files themselves are parsed
// and dispatched by internal/httpfile and internal/httpclient. Self-registers
// via init(); blank-imported in cmd/ike/main.go.
package langhttp

import (
	_ "embed"

	"ike/internal/lang"
	"ike/plugins/languages/register"
)

//go:embed queries/highlights.scm
var query string

func init() {
	register.Language(lang.Language{
		ID:          "http",
		Extensions:  []string{"http", "rest"},
		Grammar:     grammar(),
		LineComment: "#",
		// A request body is highlighted and indented as its Content-Type's
		// language (#1303/#1304); see regions.go.
		Regions: bodyRegions,
		// Whole-request folding (#1329): a "section" spans a ### separator
		// through the end of its request, so a long file of requests collapses
		// to its separator lines. The body's own structure folds too — those
		// ranges come from the embedded language's grammar via the region seam.
		FoldNodes: []string{"section"},
	})
}

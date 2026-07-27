// Package langhttp registers the .http request-file language (#1250, epic
// #1247): Tree-sitter highlighting via the rest-nvim/tree-sitter-http grammar
// (vendored C source, see grammar_cgo.go) for request lines, headers,
// comments, ### separators and placeholders. The files themselves are parsed
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
	})
}

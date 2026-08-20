package httpfile

// capture.go parses the `# @capture name = <jq-expr>` directive (#1993): the
// declarative way to feed a value out of one response into the next request
// of the same file. The parser only *recognises* the directive and attaches
// it to its request — evaluating the jq expression against a response body
// happens at dispatch time (internal/httpclient), because that is where a
// response exists.

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Capture is one `# @capture name = <jq-expr>` directive of a request block
// (#1993). Name is the variable the evaluated value is stored under, Expr the
// jq expression evaluated against the response body, and Line/EndCol locate
// the directive in the file — the anchor a failed capture is marked at: the
// 1-based line and the 0-based rune column just past its last non-space
// character.
type Capture struct {
	Name   string
	Expr   string
	Line   int
	EndCol int
}

// captureAt builds the Capture for a directive line, given its 0-based index.
// ok is false when the line is not a directive.
func captureAt(line string, idx int) (Capture, bool) {
	name, expr, ok := CaptureDirective(line)
	if !ok {
		return Capture{}, false
	}
	return Capture{
		Name:   name,
		Expr:   expr,
		Line:   idx + 1,
		EndCol: utf8.RuneCountInString(strings.TrimRight(line, " \t")),
	}, true
}

// captureRE matches a capture directive: a comment line (`#`, `##` or `//`)
// whose text is `@capture name = expression`. Three hashes are deliberately
// not accepted — `###` opens a new request block and names it, so a directive
// spelled that way would be swallowed by the separator, and the highlighter
// must not paint one either. The name follows the rules of an `@name=value`
// definition (#1867) — the two share the `{{name}}` slot — and the expression
// is everything after the `=`, trimmed, kept verbatim so a jq program may
// contain anything including further `=` characters.
var captureRE = regexp.MustCompile(`^[ \t]*(?:##?|//)[ \t]*@capture[ \t]+([A-Za-z_][A-Za-z0-9_.-]*)[ \t]*=[ \t]*(\S.*?)[ \t]*$`)

// CaptureDirective recognises a capture directive line and returns its parts
// (#1993). Exposed like VarDefinition: the highlighter and the reformatter
// must read exactly what the parser reads.
func CaptureDirective(line string) (name, expr string, ok bool) {
	m := captureRE.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

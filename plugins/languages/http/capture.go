package langhttp

// capture.go highlights the `# @capture name = <jq-expr>` directive (#1993).
// The line is a comment, so the grammar paints all of it in the comment
// colour — which is exactly wrong for a directive that decides what the *next*
// request sends. Go-produced spans win over the grammar's captures (see the
// package doc above querySpans), so the parts read apart: the `@capture`
// marker as a keyword, the variable name like any other `{{name}}`, the `=` as
// an operator, and the jq expression through the playground's own tokenizer
// (internal/jqplay), so a path, a string literal and a builtin are told apart
// here exactly as they are in the jq playground's query line.

import (
	"ike/internal/httpfile"
	"ike/internal/jqplay"
	"ike/internal/lang"
)

// captureSpans produces the directive spans of the whole buffer.
func captureSpans(lines []string) []lang.Span {
	var out []lang.Span
	for i, line := range lines {
		name, expr, ok := httpfile.CaptureDirective(line)
		if !ok {
			continue
		}
		runes := []rune(line)
		// The literal pieces are located by search rather than by re-parsing:
		// the parser already confirmed they are there in this order, and
		// their exact spelling — how much space, "#" or "//" or "###" — is
		// the author's business.
		marker := indexRunes(runes, 0, "@capture")
		if marker < 0 {
			continue
		}
		out = append(out, span(i, marker, marker+len("@capture"), "keyword"))
		nameAt := indexRunes(runes, marker+len("@capture"), name)
		if nameAt < 0 {
			continue
		}
		out = append(out, span(i, nameAt, nameAt+len([]rune(name)), "variable"))
		eq := indexRunes(runes, nameAt+len([]rune(name)), "=")
		if eq < 0 {
			continue
		}
		out = append(out, span(i, eq, eq+1, "operator"))
		exprAt := indexRunes(runes, eq+1, expr)
		if exprAt < 0 {
			continue
		}
		for _, tok := range jqplay.Tokens(expr) {
			capture, ok := jqCapture[tok.Kind]
			if !ok {
				continue
			}
			out = append(out, span(i, exprAt+tok.Start, exprAt+tok.End, capture))
		}
	}
	return out
}

// jqCapture maps the jq tokenizer's kinds onto theme captures, mirroring the
// roles the playground's query line paints (a path stands out, a string reads
// as a string, an operator recedes). KindPlain has no entry on purpose:
// unclassified text keeps the comment colour of the line it sits on.
var jqCapture = map[jqplay.Kind]string{
	jqplay.KindPath:     "property",
	jqplay.KindString:   "string",
	jqplay.KindNumber:   "number",
	jqplay.KindKeyword:  "keyword",
	jqplay.KindFunc:     "function",
	jqplay.KindVariable: "variable",
	jqplay.KindFormat:   "constant",
	jqplay.KindOperator: "punctuation",
	jqplay.KindComment:  "comment",
}

// span is the one-line span constructor this file spells out often.
func span(line, from, to int, capture string) lang.Span {
	return lang.Span{Line: line, StartCol: from, EndCol: to, Capture: capture}
}

// indexRunes returns the rune index of the first occurrence of want in runes
// at or after from, -1 when there is none.
func indexRunes(runes []rune, from int, want string) int {
	w := []rune(want)
	for i := from; i+len(w) <= len(runes); i++ {
		if string(runes[i:i+len(w)]) == want {
			return i
		}
	}
	return -1
}

package httpfile

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// triplequote.go handles the """…""" multi-line string literal API docs
// (Elasticsearch/Kibana console and friends) write inside JSON request
// bodies. The file keeps the literal as written; ResolveVars turns every
// literal of a JSON body into an ordinary JSON string before the body leaves
// ike, and the editor side (highlighting, diagnostics, reformat, masking)
// locates the literals through TripleQuoteLiterals.
//
// Conversion rules:
//   - The text's left margin is the column of its first non-blank character:
//     on the opening line, or — when the opening """ is followed only by
//     whitespace — on the first non-blank line after it. Every following line
//     loses leading whitespace up to that column; a line starting further
//     left loses only the whitespace it has. A tab counts as one column.
//   - A line break directly after the opening """ (optionally preceded by
//     whitespace) is dropped, and so are trailing whitespace plus the line
//     break directly before the closing """.
//   - The content is taken literally (backslashes are not escapes) and
//     escaped as a JSON string; {{placeholders}} stay verbatim for the
//     substitution pass.

const tripleQuote = `"""`

// TripleQuoteLiteral locates one """…""" literal in a run of lines: 0-based
// lines and byte columns. Start is the opening quotes, End is just past the
// closing quotes; an unterminated literal ends at the end of the last line.
type TripleQuoteLiteral struct {
	StartLine, StartCol int
	EndLine, EndCol     int
	Terminated          bool
}

// TripleQuoteLiterals scans JSON text for """…""" literals. Ordinary JSON
// strings are skipped (with their backslash escapes), so quotes inside them
// never open a literal. A run of more than three closing quotes closes with
// its last three, so the content may end in a quote.
func TripleQuoteLiterals(lines []string) []TripleQuoteLiteral {
	var out []TripleQuoteLiteral
	li, col := 0, 0
	for li < len(lines) {
		line := lines[li]
		if col >= len(line) {
			li, col = li+1, 0
			continue
		}
		if line[col] != '"' {
			col++
			continue
		}
		if !strings.HasPrefix(line[col:], tripleQuote) {
			col = skipJSONString(line, col)
			continue
		}
		lit := TripleQuoteLiteral{StartLine: li, StartCol: col}
		el, ec, ok := closingTripleQuote(lines, li, col+len(tripleQuote))
		if !ok {
			lit.EndLine = len(lines) - 1
			lit.EndCol = len(lines[lit.EndLine])
			return append(out, lit)
		}
		lit.EndLine, lit.EndCol, lit.Terminated = el, ec, true
		out = append(out, lit)
		li, col = el, ec
	}
	return out
}

// skipJSONString returns the column just past the ordinary JSON string
// opening at line[q], or the line's end when it is not closed on this line.
func skipJSONString(line string, q int) int {
	for i := q + 1; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(line)
}

// closingTripleQuote finds the closing """ at or after (li, col) and returns
// the position just past it.
func closingTripleQuote(lines []string, li, col int) (int, int, bool) {
	for ; li < len(lines); li, col = li+1, 0 {
		idx := strings.Index(lines[li][col:], tripleQuote)
		if idx < 0 {
			continue
		}
		end := col + idx + len(tripleQuote)
		for end < len(lines[li]) && lines[li][end] == '"' {
			end++
		}
		return li, end, true
	}
	return 0, 0, false
}

// TripleQuoteValue returns the string value of a terminated literal in lines
// after the margin and trim rules (unescaped).
func TripleQuoteValue(lines []string, lit TripleQuoteLiteral) string {
	open := lit.StartCol + len(tripleQuote)
	closeAt := lit.EndCol - len(tripleQuote)
	if lit.StartLine == lit.EndLine {
		return strings.TrimLeft(lines[lit.StartLine][open:closeAt], " \t")
	}
	first := lines[lit.StartLine][open:]
	rest := append([]string{}, lines[lit.StartLine+1:lit.EndLine]...)
	rest = append(rest, lines[lit.EndLine][:closeAt])
	if isBlank(rest[len(rest)-1]) {
		rest = rest[:len(rest)-1]
		if len(rest) == 0 {
			return strings.TrimRight(strings.TrimLeft(first, " \t"), " \t")
		}
		rest[len(rest)-1] = strings.TrimRight(rest[len(rest)-1], " \t")
	}

	var segs []string
	var margin int
	if isBlank(first) {
		margin = 0
		for _, l := range rest {
			if !isBlank(l) {
				margin = leadingBlankRunes(l)
				break
			}
		}
	} else {
		trimmed := strings.TrimLeft(first, " \t")
		margin = utf8.RuneCountInString(lines[lit.StartLine][:len(lines[lit.StartLine])-len(trimmed)])
		segs = append(segs, trimmed)
	}
	for _, l := range rest {
		segs = append(segs, dedent(l, margin))
	}
	return strings.Join(segs, "\n")
}

// isBlank reports whether s holds only spaces and tabs.
func isBlank(s string) bool { return strings.Trim(s, " \t") == "" }

// leadingBlankRunes counts the spaces and tabs s starts with.
func leadingBlankRunes(s string) int { return len(s) - len(strings.TrimLeft(s, " \t")) }

// dedent drops up to margin leading spaces/tabs from s.
func dedent(s string, margin int) string {
	i := 0
	for i < len(s) && i < margin && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[i:]
}

// ExpandTripleQuotes rewrites every terminated """…""" literal of a JSON body
// into an ordinary JSON string; all other text, and an unterminated literal,
// stays as it is.
func ExpandTripleQuotes(body string) string {
	if !strings.Contains(body, tripleQuote) {
		return body
	}
	lines := strings.Split(body, "\n")
	lits := TripleQuoteLiterals(lines)
	if len(lits) == 0 {
		return body
	}
	offsets := make([]int, len(lines))
	for i := 1; i < len(lines); i++ {
		offsets[i] = offsets[i-1] + len(lines[i-1]) + 1
	}
	var b strings.Builder
	prev := 0
	for _, lit := range lits {
		if !lit.Terminated {
			break
		}
		start := offsets[lit.StartLine] + lit.StartCol
		b.WriteString(body[prev:start])
		b.WriteString(jsonStringKeepingPlaceholders(TripleQuoteValue(lines, lit)))
		prev = offsets[lit.EndLine] + lit.EndCol
	}
	b.WriteString(body[prev:])
	return b.String()
}

// jsonStringKeepingPlaceholders quotes s as a JSON string, leaving every
// placeholder unescaped.
func jsonStringKeepingPlaceholders(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	prev := 0
	for _, m := range placeholderRE.FindAllStringIndex(s, -1) {
		b.WriteString(jsonEscape(s[prev:m[0]]))
		b.WriteString(s[m[0]:m[1]])
		prev = m[1]
	}
	b.WriteString(jsonEscape(s[prev:]))
	b.WriteByte('"')
	return b.String()
}

// jsonEscape returns s escaped as JSON string content, without the quotes
// and without HTML escaping.
func jsonEscape(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	out := strings.TrimSuffix(buf.String(), "\n")
	return out[1 : len(out)-1]
}

// isJSONBody reports whether r's inline body is a JSON document by its
// Content-Type — not a GRAPHQL or WEBSOCKET block, which own their body
// syntax.
func (r *Request) isJSONBody() bool {
	if r.GraphQL != nil || r.WebSocket != nil || r.BodyFile != "" {
		return false
	}
	ct, ok := r.Header("Content-Type")
	if !ok {
		return false
	}
	tag, ok := BodyLanguageTag(ct)
	return ok && tag == "json"
}

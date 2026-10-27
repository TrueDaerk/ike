package langansible

import (
	"strings"
	"unicode/utf8"

	"ike/internal/jinja"
	"ike/internal/lang"
)

// Jinja in Ansible YAML (#3061). Playbooks and roles are valid YAML whose
// values carry Jinja — `name: "{{ item.name | default('x') }}"` — so nothing
// is masked: the Spans hook paints the Jinja tokens over the YAML string
// they sit in, with the #3059 tokenizer. Only the tokens get a capture, so
// the blanks between them keep the string's colour.

// expressionKeys are the task keywords whose value is a bare Jinja
// expression, written without {{ }}.
var expressionKeys = map[string]bool{
	"when":         true,
	"changed_when": true,
	"failed_when":  true,
	"until":        true,
}

// jinjaSpans returns the Jinja spans of an Ansible YAML buffer: every closed
// {{ }}, {% %} and {# #} block outside a YAML comment, plus the values of the
// bare-expression keys.
func jinjaSpans(lines []string) []lang.Span {
	out := blockSpans(lines)
	return append(out, bareSpans(lines)...)
}

// blockSpans scans for Jinja blocks. An unterminated block would run to the
// end of the buffer — a stray "{{" while typing must not paint the rest of
// the playbook — so its tokens are dropped and the scan resumes on the line
// after its opener.
func blockSpans(lines []string) []lang.Span {
	var out []lang.Span
	for from := 0; from < len(lines); {
		if !hasOpener(lines[from:]) {
			break
		}
		tpl := jinja.Scan(lines[from:])
		next := len(lines)
		for _, b := range tpl.Blocks {
			line, col := tpl.Position(b.Start)
			if !b.Closed {
				next = from + line + 1
				break
			}
			if inComment(lines[from+line], col) {
				continue
			}
			for _, s := range tpl.BlockSpans(b) {
				s.Line += from
				out = append(out, s)
			}
		}
		from = next
	}
	return out
}

// hasOpener reports whether any line holds a Jinja block opener, sparing
// the scan for the common buffer without one.
func hasOpener(lines []string) bool {
	for _, l := range lines {
		if strings.Contains(l, "{{") || strings.Contains(l, "{%") || strings.Contains(l, "{#") {
			return true
		}
	}
	return false
}

// inComment reports whether rune column col of line lies in a YAML comment:
// after a "#" that starts the line or follows a blank, outside quotes.
func inComment(line string, col int) bool {
	c := commentStart(line)
	return c >= 0 && utf8.RuneCountInString(line[:c]) <= col
}

// commentStart returns the byte offset of the YAML comment on line, -1 when
// it has none.
func commentStart(line string) int {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			// A quote opens a string only at the start of a scalar;
			// "it's" in a plain scalar is an apostrophe.
			if i == 0 || strings.ContainsRune(" \t:-[{,", rune(line[i-1])) {
				quote = c
			}
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return i
		}
	}
	return -1
}

// bareSpans tokenizes the values of the bare-expression keys: an inline
// value (`when: x is defined`, quoted or plain), the items of a list below
// the key, or the lines of a block scalar. A value already written with
// {{ }} is left to blockSpans.
func bareSpans(lines []string) []lang.Span {
	var out []lang.Span
	for i := 0; i < len(lines); i++ {
		keyCol, valCol, ok := expressionKey(lines[i])
		if !ok {
			continue
		}
		val := strings.TrimSpace(stripComment(lines[i][valCol:]))
		switch {
		case val == "", val[0] == '|', val[0] == '>':
			block := val != ""
			for i+1 < len(lines) {
				next := lines[i+1]
				body := strings.TrimLeft(next, " \t")
				if body == "" {
					i++
					continue
				}
				indent := len(next) - len(body)
				// A list may sit at the key's own indent (compact YAML).
				item := !block && strings.HasPrefix(body, "- ")
				if indent < keyCol || indent == keyCol && !item {
					break
				}
				i++
				if strings.HasPrefix(body, "#") {
					continue
				}
				start := indent
				if item {
					start += 2
				} else if !block {
					continue // a nested mapping: not an expression list
				}
				out = append(out, exprSpans(i, next, start)...)
			}
		default:
			out = append(out, exprSpans(i, lines[i], valCol)...)
		}
	}
	return out
}

// expressionKey matches a line holding one of the expressionKeys — as a
// mapping key or the first key of a list item — and returns the byte column
// of the key and the one just past its colon.
func expressionKey(line string) (int, int, bool) {
	body := strings.TrimLeft(line, " \t")
	col := len(line) - len(body)
	if strings.HasPrefix(body, "- ") {
		trimmed := strings.TrimLeft(body[2:], " ")
		col += len(body) - len(trimmed)
		body = trimmed
	}
	colon := strings.IndexByte(body, ':')
	if colon < 0 || !expressionKeys[body[:colon]] {
		return 0, 0, false
	}
	if rest := body[colon+1:]; rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, 0, false
	}
	return col, col + colon + 1, true
}

// stripComment cuts the YAML comment off s.
func stripComment(s string) string {
	if c := commentStart(s); c >= 0 {
		return s[:c]
	}
	return s
}

// exprSpans tokenizes the expression on line (buffer line n) from byte
// column start: comment stripped, surrounding quotes peeled. A value holding
// a {{ }} or {% %} block, or a flow sequence, is left alone.
func exprSpans(n int, line string, start int) []lang.Span {
	val := stripComment(line[start:])
	lead := len(val) - len(strings.TrimLeft(val, " \t"))
	val = strings.TrimRight(val[lead:], " \t")
	start += lead
	if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
		val = val[1 : len(val)-1]
		start++
	}
	if val == "" || val[0] == '[' || strings.Contains(val, "{{") || strings.Contains(val, "{%") {
		return nil
	}
	base := utf8.RuneCountInString(line[:start])
	toks := jinja.ExpressionTokens(val)
	out := make([]lang.Span, 0, len(toks))
	for _, t := range toks {
		col := base + utf8.RuneCountInString(val[:t.Start])
		out = append(out, lang.Span{
			Line:     n,
			StartCol: col,
			EndCol:   col + utf8.RuneCountInString(val[t.Start:t.End]),
			Capture:  t.Class.Capture(),
		})
	}
	return out
}

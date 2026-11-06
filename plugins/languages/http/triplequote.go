package langhttp

// triplequote.go makes """…""" string literals inside JSON request bodies
// read as strings. The JSON grammar and a JSON language server cannot parse
// them, so a JSON body holding one hands the embedded language a same-shaped
// stand-in text (lang.Region.Text): each terminated literal becomes an
// ordinary JSON string on its opening line with the rest of it blanked, and
// an unterminated one becomes an empty string followed by the closers of
// every bracket still open before it. Every consumer of the region — the
// injected highlighting, the LSP fragment document, the region masks — reads
// that stand-in, so the body parses cleanly while its other content keeps
// its JSON highlighting and diagnostics. On top of it, this file lays the
// string capture over every line of each literal, stretches a mask the JSON
// producer puts on a literal's stand-in over the literal's real content, and
// reports an unterminated literal as one error at its opening quotes.
//
// Sending converts the same literals with httpfile.ExpandTripleQuotes.

import (
	"strings"
	"unicode/utf8"

	"ike/internal/httpfile"
	"ike/internal/lang"
	"ike/internal/secret"
)

// bodyLiteral is one """…""" literal in host coordinates: 0-based lines and
// rune columns, end exclusive.
type bodyLiteral struct {
	sl, sc, el, ec int
	terminated     bool
}

// literalsIn returns the literals of the body on lines start..end.
func literalsIn(lines []string, start, end int) []bodyLiteral {
	body := lines[start : end+1]
	found := false
	for _, l := range body {
		if strings.Contains(l, `"""`) {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	var out []bodyLiteral
	for _, lit := range httpfile.TripleQuoteLiterals(body) {
		sl, el := start+lit.StartLine, start+lit.EndLine
		out = append(out, bodyLiteral{
			sl: sl, sc: utf8.RuneCountInString(lines[sl][:lit.StartCol]),
			el: el, ec: utf8.RuneCountInString(lines[el][:lit.EndCol]),
			terminated: lit.Terminated,
		})
	}
	return out
}

// jsonBodyLiterals returns the literals of every JSON body region that
// carries a stand-in text.
func jsonBodyLiterals(regions []lang.Region, lines []string) []bodyLiteral {
	var out []bodyLiteral
	for _, r := range regions {
		if r.Lang == "json" && r.Text != nil {
			out = append(out, literalsIn(lines, r.StartLine, r.EndLine)...)
		}
	}
	return out
}

// literalStandIn returns the JSON-parseable stand-in for the body on lines
// start..end holding lits: same line count, same rune count per line.
func literalStandIn(lines []string, start, end int, lits []bodyLiteral) []string {
	rows := make([][]rune, end-start+1)
	for i := range rows {
		rows[i] = []rune(lines[start+i])
	}
	for _, lit := range lits {
		sl, el := lit.sl-start, lit.el-start
		var cells [][2]int
		for li := sl; li <= el; li++ {
			from, to := 0, len(rows[li])
			if li == sl {
				from = lit.sc
			}
			if li == el {
				to = lit.ec
			}
			for c := from; c < to; c++ {
				rows[li][c] = ' '
				cells = append(cells, [2]int{li, c})
			}
		}
		if lit.terminated {
			last := len(rows[sl]) - 1
			if el == sl {
				last = lit.ec - 1
			}
			rows[sl][lit.sc], rows[sl][last] = '"', '"'
			continue
		}
		for i, r := range []rune(`""` + openClosers(rows, sl, lit.sc)) {
			if i >= len(cells) {
				break
			}
			rows[cells[i][0]][cells[i][1]] = r
		}
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = string(r)
	}
	return out
}

// openClosers returns the closing brackets for every bracket still open in
// rows before (line, col), innermost first; ordinary strings are skipped.
func openClosers(rows [][]rune, line, col int) string {
	var stack []rune
	for li := 0; li <= line; li++ {
		row := rows[li]
		end := len(row)
		if li == line {
			end = col
		}
		for i := 0; i < end; i++ {
			switch row[i] {
			case '"':
				for i++; i < end && row[i] != '"'; i++ {
					if row[i] == '\\' {
						i++
					}
				}
			case '{':
				stack = append(stack, '}')
			case '[':
				stack = append(stack, ']')
			case '}', ']':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
		}
	}
	out := make([]rune, len(stack))
	for i, r := range stack {
		out[len(stack)-1-i] = r
	}
	return string(out)
}

// literalStringSpans returns the string capture over every line of each
// literal; on continuation lines it starts at the first non-blank rune.
func literalStringSpans(lits []bodyLiteral, lines []string) []lang.Span {
	var out []lang.Span
	for _, lit := range lits {
		forEachLiteralLine(lit, lines, func(li, from, to int) {
			if li != lit.sl {
				from = firstNonBlank(lines[li], from, to)
			}
			if from < to {
				out = append(out, lang.Span{Line: li, StartCol: from, EndCol: to, Capture: "string"})
			}
		})
	}
	return out
}

// expandLiteralMasks replaces a mask covering a terminated literal's
// stand-in string with masks over the literal's content on each of its
// lines; other masks pass through.
func expandLiteralMasks(masks []lang.Span, lits []bodyLiteral, lines []string) []lang.Span {
	if len(lits) == 0 {
		return masks
	}
	out := masks[:0:0]
	for _, m := range masks {
		lit, ok := literalAt(lits, m.Line, m.StartCol)
		if !ok {
			out = append(out, m)
			continue
		}
		forEachLiteralLine(lit, lines, func(li, from, to int) {
			if li == lit.sl {
				from += 3
			} else {
				from = firstNonBlank(lines[li], from, to)
			}
			if li == lit.el {
				to -= 3
			}
			if from < to {
				out = append(out, secret.Span(li, from, to))
			}
		})
	}
	return out
}

// literalAt returns the terminated literal whose stand-in content starts at
// (line, col).
func literalAt(lits []bodyLiteral, line, col int) (bodyLiteral, bool) {
	for _, lit := range lits {
		if lit.terminated && lit.sl == line && lit.sc+1 == col {
			return lit, true
		}
	}
	return bodyLiteral{}, false
}

// forEachLiteralLine calls fn with the rune range [from, to) the literal
// covers on each of its lines.
func forEachLiteralLine(lit bodyLiteral, lines []string, fn func(li, from, to int)) {
	for li := lit.sl; li <= lit.el && li < len(lines); li++ {
		from, to := 0, utf8.RuneCountInString(lines[li])
		if li == lit.sl {
			from = lit.sc
		}
		if li == lit.el {
			to = lit.ec
		}
		fn(li, from, to)
	}
}

// firstNonBlank returns the first rune column in [from, to) of line that is
// not a space or tab; to when there is none.
func firstNonBlank(line string, from, to int) int {
	runes := []rune(line)
	for c := from; c < to && c < len(runes); c++ {
		if runes[c] != ' ' && runes[c] != '\t' {
			return c
		}
	}
	return to
}

// httpLint is the lang.Language.Lint hook: an unterminated """ in a JSON
// body is one error at its opening quotes.
func httpLint(_ string, lines []string) []lang.Note {
	f := httpfile.Parse(strings.Join(lines, "\n"))
	var out []lang.Note
	for _, lit := range jsonBodyLiterals(regionsFor(f, lines), lines) {
		if !lit.terminated {
			out = append(out, lang.Note{
				Line: lit.sl, StartCol: lit.sc, EndCol: lit.sc + 3,
				Severity: lang.NoteError,
				Message:  `unterminated triple-quoted string: missing closing """`,
			})
		}
	}
	return out
}

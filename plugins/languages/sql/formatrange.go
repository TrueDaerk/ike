package langsql

import "strings"

// formatrange.go: reformat-selection for the built-in SQL formatter (#1403)
// formats the statements overlapping the selected lines and leaves the rest
// of the buffer byte-identical.

// stmtSpan is one statement's 0-based inclusive line span, including the
// standalone comments directly above it (they format with their statement).
type stmtSpan struct{ first, last int }

// statementSpans groups the token stream into per-statement line spans.
func statementSpans(toks []tok) []stmtSpan {
	var spans []stmtSpan
	open := false
	var cur stmtSpan
	for i, t := range toks {
		if !open {
			cur = stmtSpan{first: t.line, last: t.line}
			open = true
		}
		last := t.line + strings.Count(t.text, "\n")
		if last > cur.last {
			cur.last = last
		}
		if t.kind == tSemi {
			// trailing same-line comments belong to this statement
			j := i + 1
			for j < len(toks) && toks[j].nl == 0 &&
				(toks[j].kind == tLineComment || toks[j].kind == tBlockComment) {
				if toks[j].line > cur.last {
					cur.last = toks[j].line
				}
				j++
			}
			spans = append(spans, cur)
			open = false
		}
	}
	if open {
		spans = append(spans, cur)
	}
	return spans
}

// formatRangeSQL formats the statements overlapping [startLine, endLine]
// (0-based inclusive) and splices them back; everything else is untouched.
func formatRangeSQL(text string, startLine, endLine int, opts sqlOptions) (string, error) {
	toks, ok := lexSQL(text)
	if !ok || unbalanced(toks) {
		return "", errMalformed
	}
	if bad, checked := parseHasErrors(text); checked && bad {
		return "", errMalformed
	}
	spans := statementSpans(toks)
	// merge statements sharing lines with a selected one (they must splice
	// together — two statements on one line cannot be edited independently)
	sel := map[int]bool{}
	changed := true
	for changed {
		changed = false
		covered := map[int]bool{}
		for i, sp := range spans {
			if sel[i] || (sp.last >= startLine && sp.first <= endLine) {
				if !sel[i] {
					sel[i] = true
					changed = true
				}
				for l := sp.first; l <= sp.last; l++ {
					covered[l] = true
				}
			}
		}
		for i, sp := range spans {
			if !sel[i] && (covered[sp.first] || covered[sp.last]) {
				sel[i] = true
				changed = true
			}
		}
	}
	if len(sel) == 0 {
		return text, nil
	}
	lines := strings.Split(text, "\n")
	// contiguous selected groups, back to front so line numbers stay valid
	type group struct{ first, last int }
	var groups []group
	for i := 0; i < len(spans); i++ {
		if !sel[i] {
			continue
		}
		g := group{first: spans[i].first, last: spans[i].last}
		for i+1 < len(spans) && sel[i+1] {
			i++
			if spans[i].last > g.last {
				g.last = spans[i].last
			}
		}
		groups = append(groups, g)
	}
	for gi := len(groups) - 1; gi >= 0; gi-- {
		g := groups[gi]
		if g.last >= len(lines) {
			g.last = len(lines) - 1
		}
		src := strings.Join(lines[g.first:g.last+1], "\n")
		formatted, err := formatSQL(src, opts)
		if err != nil {
			return "", err
		}
		formatted = strings.TrimSuffix(formatted, "\n")
		out := strings.Split(formatted, "\n")
		lines = append(lines[:g.first], append(out, lines[g.last+1:]...)...)
	}
	return strings.Join(lines, "\n"), nil
}

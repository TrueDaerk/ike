package complexity

import "strings"

// comments is a language's comment syntax, as lang.Language declares it:
// the line marker ("//", "#") and the block open/close pair; either may be
// empty.
type comments struct {
	line  string
	block [2]string
}

// nloc classifies the lines in lines[from..to] (inclusive, clamped): code
// counts the lines that carry code — not blank, and not only a comment —
// comment the comment-only lines, which are blank once their comments are
// stripped but were not blank before. A line with code and a trailing
// comment counts as code. A line comment hides the rest of its line; a
// block comment hides everything up to its close, across lines, so every
// line it covers is a comment line unless it also carries code.
// Comment markers inside string literals are not recognised — a line that
// also carries code before the marker still counts, so the error is confined
// to a line that is nothing but a string starting with a marker.
func nloc(lines []string, from, to int, cm comments) (code, comment int) {
	if from < 0 {
		from = 0
	}
	if to >= len(lines) {
		to = len(lines) - 1
	}
	open, close := cm.block[0], cm.block[1]
	if close == "" {
		open = ""
	}
	inBlock := false
	for i := from; i <= to; i++ {
		rest := lines[i]
		blank := strings.TrimSpace(rest) == ""
		hasCode := false
		for rest != "" {
			if inBlock {
				idx := strings.Index(rest, close)
				if idx < 0 {
					rest = ""
					break
				}
				rest = rest[idx+len(close):]
				inBlock = false
				continue
			}
			li, bi := -1, -1
			if cm.line != "" {
				li = strings.Index(rest, cm.line)
			}
			if open != "" {
				bi = strings.Index(rest, open)
			}
			switch {
			case li < 0 && bi < 0:
				hasCode = hasCode || strings.TrimSpace(rest) != ""
				rest = ""
			case li >= 0 && (bi < 0 || li < bi):
				hasCode = hasCode || strings.TrimSpace(rest[:li]) != ""
				rest = ""
			default:
				hasCode = hasCode || strings.TrimSpace(rest[:bi]) != ""
				rest = rest[bi+len(open):]
				inBlock = true
			}
		}
		switch {
		case hasCode:
			code++
		case !blank:
			comment++
		}
	}
	return code, comment
}

// density is the comment share of a body: comment / (code + comment), 0
// when both are 0.
func density(code, comment int) float64 {
	if code+comment == 0 {
		return 0
	}
	return float64(comment) / float64(code+comment)
}

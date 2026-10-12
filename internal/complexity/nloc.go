package complexity

import "strings"

// comments is a language's comment syntax, as lang.Language declares it:
// the line marker ("//", "#") and the block open/close pair; either may be
// empty.
type comments struct {
	line  string
	block [2]string
}

// nloc counts the lines in lines[from..to] (inclusive, clamped) that carry
// code: not blank, and not only a comment. A line comment hides the rest of
// its line; a block comment hides everything up to its close, across lines.
// Comment markers inside string literals are not recognised — a line that
// also carries code before the marker still counts, so the error is confined
// to a line that is nothing but a string starting with a marker.
func nloc(lines []string, from, to int, cm comments) int {
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
	n := 0
	inBlock := false
	for i := from; i <= to; i++ {
		rest := lines[i]
		code := false
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
				code = code || strings.TrimSpace(rest) != ""
				rest = ""
			case li >= 0 && (bi < 0 || li < bi):
				code = code || strings.TrimSpace(rest[:li]) != ""
				rest = ""
			default:
				code = code || strings.TrimSpace(rest[:bi]) != ""
				rest = rest[bi+len(open):]
				inBlock = true
			}
		}
		if code {
			n++
		}
	}
	return n
}

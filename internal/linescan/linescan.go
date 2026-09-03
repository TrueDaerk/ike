// Package linescan holds tiny rune-slice scanning helpers shared by the
// line-oriented hint families (cronhint, permhint): splitting a line into
// whitespace-separated words, finding the start of a trailing "# comment" on
// a YAML line, and testing for ASCII space/tab. None of it is Unicode-aware
// beyond operating on runes — these are one-line config formats, not prose.
package linescan

// Words returns the [start, end) rune-column ranges of the whitespace-
// separated words of runes from index i on.
func Words(runes []rune, i int) [][2]int {
	var out [][2]int
	for i < len(runes) {
		i = SkipSpace(runes, i)
		if i >= len(runes) {
			break
		}
		j := i
		for j < len(runes) && !IsSpace(runes[j]) {
			j++
		}
		out = append(out, [2]int{i, j})
		i = j
	}
	return out
}

// CommentStart returns the column of a trailing " #" comment on a YAML line,
// or the line end. Quoted regions are respected so a "#" inside a scalar does
// not truncate the value.
func CommentStart(runes []rune, from int) int {
	var quote rune
	for i := from; i < len(runes); i++ {
		switch {
		case quote != 0:
			if runes[i] == quote {
				quote = 0
			}
		case runes[i] == '"' || runes[i] == '\'':
			quote = runes[i]
		case runes[i] == '#' && i > from && IsSpace(runes[i-1]):
			return i
		}
	}
	return len(runes)
}

// SkipSpace advances i past a run of spaces/tabs.
func SkipSpace(runes []rune, i int) int {
	for i < len(runes) && IsSpace(runes[i]) {
		i++
	}
	return i
}

// IsSpace reports whether r is an ASCII space or tab.
func IsSpace(r rune) bool { return r == ' ' || r == '\t' }

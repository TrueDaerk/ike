package format

import "strings"

// EditsForResult normalizes a provider Result into applicable edits against
// the request's buffer snapshot. Edit-returning providers pass through;
// text-returning providers get a minimal line diff — the common unchanged
// prefix and suffix are trimmed and only the middle is replaced, so the
// cursor clamp after ApplyTextEdits keeps the caret in place for local
// changes instead of jumping on a whole-file replace. nil means "no changes".
func EditsForResult(lines []string, res Result) []Edit {
	if res.Edits != nil {
		return res.Edits
	}
	if res.Text == nil {
		return nil
	}
	return diffLines(lines, strings.Split(*res.Text, "\n"))
}

// diffLines computes the single replace edit turning old into new: trim the
// common line prefix and suffix, replace what remains.
func diffLines(old, new []string) []Edit {
	if len(old) == 0 {
		old = []string{""}
	}
	p := 0
	for p < len(old) && p < len(new) && old[p] == new[p] {
		p++
	}
	s := 0
	for s < len(old)-p && s < len(new)-p && old[len(old)-1-s] == new[len(new)-1-s] {
		s++
	}
	if p == len(old) && p == len(new) {
		return nil // identical
	}
	mid := new[p : len(new)-s]
	e := Edit{StartLine: p, StartCol: 0}
	if s > 0 {
		// The kept suffix starts a line of its own: replace whole lines up to
		// it, newline-terminated text keeps the boundary intact.
		e.EndLine, e.EndCol = len(old)-s, 0
		if len(mid) > 0 {
			e.Text = strings.Join(mid, "\n") + "\n"
		}
		return []Edit{e}
	}
	// No kept suffix: the edit runs to the end of the last line.
	last := len(old) - 1
	e.EndLine, e.EndCol = last, len([]rune(old[last]))
	if p == len(old) {
		// Pure append: anchor the insertion at the end of the buffer.
		e.StartLine, e.StartCol = last, len([]rune(old[last]))
		e.Text = "\n" + strings.Join(mid, "\n")
		return []Edit{e}
	}
	if len(mid) == 0 && p > 0 {
		// Pure deletion of the trailing lines: the separating newline before
		// them must go too, so the edit starts at the previous line's end.
		e.StartLine, e.StartCol = p-1, len([]rune(old[p-1]))
		return []Edit{e}
	}
	e.Text = strings.Join(mid, "\n")
	return []Edit{e}
}

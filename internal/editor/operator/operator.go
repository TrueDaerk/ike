// Package operator applies vim operators (d c y p) over a resolved Target —
// a charwise range or a span of whole lines — writing yanked/deleted text to the
// register store and recording every mutation in a history.Recorder so undo and
// "." work uniformly. Operators never read keys or modes; the editor resolves a
// motion or text object into a Target and hands it here.
package operator

import "ike/internal/editor/buffer"

// Target is what an operator acts on: either a charwise half-open Range or, when
// Linewise, the whole lines from Range.Start.Line to Range.End.Line inclusive.
type Target struct {
	Range    buffer.Range
	Linewise bool
}

// CharTarget builds a charwise target over r.
func CharTarget(r buffer.Range) Target { return Target{Range: r} }

// LineTarget builds a linewise target spanning lines a..b inclusive.
func LineTarget(a, b int) Target {
	if b < a {
		a, b = b, a
	}
	return Target{Range: buffer.Range{Start: buffer.Position{Line: a}, End: buffer.Position{Line: b}}, Linewise: true}
}

// firstNonBlankCol returns the column of the first non-blank rune of line i, or
// 0 for a blank/empty line.
func firstNonBlankCol(b *buffer.Buffer, i int) int {
	r := []rune(b.Line(i))
	c := 0
	for c < len(r) && (r[c] == ' ' || r[c] == '\t') {
		c++
	}
	if c >= len(r) {
		return 0
	}
	return c
}

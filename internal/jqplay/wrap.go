package jqplay

// wrap.go breaks a jq program into the rows of the playground's expanded
// query view (#2032). The query line is one windowed row by default, so a
// pipeline wider than the pane is only ever visible in pieces; expanding it
// lays the same program out over several rows, and this is where the break
// points come from.
//
// The rule is "break at the pipes, fall back to the width": a jq pipeline
// reads as a sequence of stages, so a row boundary that coincides with a `|`
// keeps a stage whole and lets the eye follow the data. A stage that is
// itself wider than the row is cut at the width — there is nothing better to
// break it on, and refusing to break it would hide text again.
//
// The result is expressed in *rune* index ranges into the program, not in
// substrings: the renderer colors and cursors rune by rune (highlight.go's
// tokens are rune-indexed too), so handing it strings would only mean
// converting the indices back. The same ranges carry the caret across the rows
// once the view is editable (#2038) — RowCol/PosAt are the coordinate pair a
// vertical motion is made of.

// Line is one visual row of a wrapped program: the rune range [Start, End) of
// the program it shows. Ranges are ordered and never overlap; the blanks that
// separated a row from the pipe before it are dropped, so no row starts on a
// space.
type Line struct {
	Start int
	End   int
}

// Wrap lays program out over rows at most width runes wide, breaking after a
// top-level `|` where one fits and at the width otherwise. It always returns
// at least one line (the empty program is one empty row), so a caller may
// index the first row without a length check.
func Wrap(program string, width int) []Line {
	r := []rune(program)
	if width < 1 {
		width = 1
	}
	var out []Line
	cur := Line{}
	open := false
	flush := func() {
		if open {
			out = append(out, cur)
			open = false
		}
	}
	for _, seg := range pipeSegments(program, r) {
		start, end := seg.Start, seg.End
		atStage := true
		for start < end {
			if !open {
				// A row that begins a stage does not start on the blanks that
				// separated it from the pipe before it. A row that continues
				// one (a stage cut at the width) keeps every rune — the cut
				// may land inside a string literal, whose spaces are text.
				if atStage {
					for start < end && (r[start] == ' ' || r[start] == '\t') {
						start++
					}
					if start >= end {
						break
					}
				}
				cur, open = Line{Start: start, End: start}, true
				atStage = false
			}
			room := width - (cur.End - cur.Start)
			take := end - start
			if take > room {
				if cur.End > cur.Start {
					// Something is on the row already: the stage moves down
					// whole instead of being split across the boundary.
					flush()
					continue
				}
				take = room // the stage alone is wider than the row
			}
			cur.End = start + take
			start += take
			if cur.End-cur.Start >= width {
				flush()
			}
		}
	}
	flush()
	if len(out) == 0 {
		out = []Line{{Start: 0, End: 0}}
	}
	return out
}

// LineAt reports the index of the line holding rune position pos — the row the
// cursor sits on. A position in a dropped blank run resolves to the row that
// follows it, and a position past the last rune to the last row, so the answer
// is always a valid index into lines.
func LineAt(lines []Line, pos int) int {
	for i, l := range lines {
		if pos < l.End || (pos == l.End && i == len(lines)-1) {
			return i
		}
	}
	if len(lines) == 0 {
		return 0
	}
	return len(lines) - 1
}

// pipeSegments splits the program after every top-level `|`, so each segment
// is one pipeline stage including the pipe that ends it. The split uses the
// scanner's tokens rather than a rune search: a `|` inside a string literal or
// a comment is text, not a stage boundary, and `||` is the or-operator.
func pipeSegments(program string, r []rune) []Line {
	var out []Line
	start := 0
	tokens := Tokens(program)
	for _, t := range tokens {
		if t.Kind != KindOperator || t.End != t.Start+1 || r[t.Start] != '|' {
			continue
		}
		if (t.Start > 0 && r[t.Start-1] == '|') || (t.Start+1 < len(r) && r[t.Start+1] == '|') {
			continue // `||` is one operator, not two stage boundaries
		}
		out = append(out, Line{Start: start, End: t.Start + 1})
		start = t.Start + 1
	}
	if start < len(r) || len(out) == 0 {
		out = append(out, Line{Start: start, End: len(r)})
	}
	return out
}

// RowCol resolves rune position pos into the row that holds it and the column
// within that row — the *caret's* coordinates in the expanded query view
// (#2038), which is what a vertical motion has to work in.
//
// It differs from LineAt in one case, and that case is the whole reason it
// exists: a caret standing on the end of a row the wrap broke at a pipe
// belongs to **that** row, past its last rune, not to the row below. LineAt
// answers by containment and hands such a position to the following row, which
// would make ↑ from the row below land on a position that reads as being on
// the row below again — a motion that never arrives. The distinction is safe
// exactly where the wrap dropped a blank between the two rows; a row cut at
// the width touches the next one, and there the position is the next row's
// first cell, which is a real cell and unambiguous.
//
// A position inside a dropped blank run answers column 0 of the row that
// follows it, the cell the renderer draws the caret on.
func RowCol(lines []Line, pos int) (row, col int) {
	row = LineAt(lines, pos)
	if row >= len(lines) {
		return row, 0
	}
	if row > 0 && pos == lines[row-1].End && lines[row-1].End < lines[row].Start {
		row--
	}
	col = pos - lines[row].Start
	if col < 0 {
		col = 0
	}
	if w := lines[row].End - lines[row].Start; col > w {
		col = w
	}
	return row, col
}

// PosAt is the inverse of RowCol: the rune position at column col of row,
// clamped into the row and into the program. A vertical motion keeps its goal
// column through short rows this way — the column is remembered, the position
// it resolves to is not.
func PosAt(lines []Line, row, col int) int {
	if len(lines) == 0 {
		return 0
	}
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}
	if col < 0 {
		col = 0
	}
	pos := lines[row].Start + col
	if pos > lines[row].End {
		pos = lines[row].End
	}
	return pos
}

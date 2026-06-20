// Package buffer holds the editor's text storage: a line slice ([]string) with
// rune-aware position arithmetic and primitive edits expressed as range
// replacements. It is the single place that maps between rune columns (what
// motions and the cursor use) and byte offsets (what string slicing touches);
// no other package performs rune/byte arithmetic.
package buffer

// Position is a cursor location: a 0-based line and a 0-based rune column. Col
// may equal the line's rune length (one past the last rune) for insert-mode and
// exclusive-motion endpoints.
type Position struct {
	Line int
	Col  int
}

// Before reports whether p sorts strictly before q in reading order.
func (p Position) Before(q Position) bool {
	if p.Line != q.Line {
		return p.Line < q.Line
	}
	return p.Col < q.Col
}

// Equal reports whether p and q are the same location.
func (p Position) Equal(q Position) bool { return p.Line == q.Line && p.Col == q.Col }

// Min returns the earlier of p and q in reading order.
func Min(p, q Position) Position {
	if p.Before(q) {
		return p
	}
	return q
}

// Max returns the later of p and q in reading order.
func Max(p, q Position) Position {
	if p.Before(q) {
		return q
	}
	return p
}

// Range is a half-open span of text between Start (inclusive) and End
// (exclusive), Start never after End. A Range is always stored normalized; use
// NewRange to order two arbitrary positions.
type Range struct {
	Start Position
	End   Position
}

// NewRange returns the Range covering a and b, normalized so Start <= End.
func NewRange(a, b Position) Range {
	if b.Before(a) {
		a, b = b, a
	}
	return Range{Start: a, End: b}
}

// Empty reports whether the range covers no text.
func (r Range) Empty() bool { return r.Start.Equal(r.End) }

// byteOffset maps a rune column within line to a byte offset, clamping a column
// past the end to the line's byte length. It is the sole rune->byte mapping.
func byteOffset(line string, col int) int {
	if col <= 0 {
		return 0
	}
	n := 0
	for i := range line { // ranging a string yields byte index i at each rune start
		if n == col {
			return i
		}
		n++
	}
	return len(line)
}

// runeLen returns the number of runes in s.
func runeLen(s string) int { return len([]rune(s)) }

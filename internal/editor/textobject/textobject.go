// Package textobject resolves vim text objects to a buffer Range, so operators
// can act on "the thing" under the cursor (a word, a quoted string, a bracketed
// span) rather than a motion target. Each resolver returns the half-open range
// to operate on plus ok=false when no object is found, leaving the operator a
// no-op. Inner ("i") excludes the delimiters; around ("a") includes them.
package textobject

import "ike/internal/editor/buffer"

// Result is a resolved text object.
type Result struct {
	Range buffer.Range
	OK    bool
}

// runeAt returns the rune at p, '\n' at a newline slot, or 0 at end-of-buffer.
func runeAt(b *buffer.Buffer, p buffer.Position) rune {
	if p.Col < b.RuneLen(p.Line) {
		return []rune(b.Line(p.Line))[p.Col]
	}
	if p.Line < b.LineCount()-1 {
		return '\n'
	}
	return 0
}

func next(b *buffer.Buffer, p buffer.Position) (buffer.Position, bool) {
	if p.Col < b.RuneLen(p.Line) {
		return buffer.Position{Line: p.Line, Col: p.Col + 1}, true
	}
	if p.Line < b.LineCount()-1 {
		return buffer.Position{Line: p.Line + 1, Col: 0}, true
	}
	return p, false
}

func prev(b *buffer.Buffer, p buffer.Position) (buffer.Position, bool) {
	if p.Col > 0 {
		return buffer.Position{Line: p.Line, Col: p.Col - 1}, true
	}
	if p.Line > 0 {
		return buffer.Position{Line: p.Line - 1, Col: b.RuneLen(p.Line - 1)}, true
	}
	return p, false
}

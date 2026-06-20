package motion

import "ike/internal/editor/buffer"

// pairs maps each bracket to its partner and whether it opens (scan forward) or
// closes (scan backward).
var openOf = map[rune]rune{')': '(', ']': '[', '}': '{'}
var closeOf = map[rune]rune{'(': ')', '[': ']', '{': '}'}

// MatchPair implements "%": from the cursor, find the first bracket at or after
// the cursor on its line, then jump to the matching bracket, honouring nesting.
// ok is false when no bracket is on the line or no match is found. Inclusive.
func MatchPair(b *buffer.Buffer, from buffer.Position, count int) (Result, bool) {
	line := []rune(b.Line(from.Line))
	col := from.Col
	// Locate the bracket to match: the first one at or after the cursor column.
	for col < len(line) {
		if _, ok := closeOf[line[col]]; ok {
			return scanForward(b, from.Line, col, line[col])
		}
		if _, ok := openOf[line[col]]; ok {
			return scanBackward(b, from.Line, col, line[col])
		}
		col++
	}
	return Result{Pos: from}, false
}

func scanForward(b *buffer.Buffer, line, col int, open rune) (Result, bool) {
	close := closeOf[open]
	depth := 0
	p := buffer.Position{Line: line, Col: col}
	for {
		r := runeAt(b, p)
		switch r {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return Result{Pos: p, Kind: Inclusive}, true
			}
		}
		q, ok := next(b, p)
		if !ok {
			return Result{Pos: buffer.Position{Line: line, Col: col}}, false
		}
		p = q
	}
}

func scanBackward(b *buffer.Buffer, line, col int, close rune) (Result, bool) {
	open := openOf[close]
	depth := 0
	p := buffer.Position{Line: line, Col: col}
	for {
		r := runeAt(b, p)
		switch r {
		case close:
			depth++
		case open:
			depth--
			if depth == 0 {
				return Result{Pos: p, Kind: Inclusive}, true
			}
		}
		q, ok := prev(b, p)
		if !ok {
			return Result{Pos: buffer.Position{Line: line, Col: col}}, false
		}
		p = q
	}
}

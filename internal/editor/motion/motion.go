// Package motion implements vim motions. A motion maps a starting position and
// a count to a target position plus a Kind (charwise exclusive/inclusive or
// linewise). Returning a target+kind — rather than mutating anything — is what
// lets the same motion drive both plain cursor movement and operator+motion
// composition: the operator consumes (from, target, kind) uniformly.
package motion

import "ike/internal/editor/buffer"

// Kind classifies how an operator should treat the span a motion covers.
type Kind int

const (
	// Exclusive: the span stops before the target rune (w, b, 0, h, l, {, }).
	Exclusive Kind = iota
	// Inclusive: the span includes the target rune ($, e, f, t, %).
	Inclusive
	// Linewise: whole lines from the start line to the target line (j, k, gg, G).
	Linewise
)

// Result is a motion's outcome.
type Result struct {
	Pos  buffer.Position
	Kind Kind
	// Jump marks a large motion (gg, G) whose departure point belongs in the
	// navigation history (Roadmap 0220). Set by the key layer, consumed when
	// the motion is applied as a move (operators do not jump).
	Jump bool
}

// charClass partitions runes for word motions: blank, word (alnum + '_'), or
// other (punctuation). A WORD motion collapses word and other into one
// non-blank class.
type charClass int

const (
	clsBlank charClass = iota
	clsWord
	clsPunct
)

func classify(r rune, big bool) charClass {
	switch {
	case r == ' ' || r == '\t' || r == '\n' || r == 0:
		return clsBlank
	case big:
		return clsPunct // any non-blank is the same class for WORD motions
	case r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		r > 127: // treat non-ASCII as word chars (keeps identifiers intact)
		return clsWord
	default:
		return clsPunct
	}
}

// runeAt returns the rune at p, or '\n' at a line's newline slot (col == line
// length). At end-of-buffer it returns 0 (blank).
func runeAt(b *buffer.Buffer, p buffer.Position) rune {
	if p.Col < b.RuneLen(p.Line) {
		return []rune(b.Line(p.Line))[p.Col]
	}
	if p.Line < b.LineCount()-1 {
		return '\n'
	}
	return 0
}

func classAt(b *buffer.Buffer, p buffer.Position, big bool) charClass {
	return classify(runeAt(b, p), big)
}

// next advances one position, crossing the newline slot into the next line. At
// end-of-buffer it returns the same position with ok=false.
func next(b *buffer.Buffer, p buffer.Position) (buffer.Position, bool) {
	if p.Col < b.RuneLen(p.Line) {
		return buffer.Position{Line: p.Line, Col: p.Col + 1}, true
	}
	if p.Line < b.LineCount()-1 {
		return buffer.Position{Line: p.Line + 1, Col: 0}, true
	}
	return p, false
}

// prev steps one position back, crossing into the previous line's newline slot.
func prev(b *buffer.Buffer, p buffer.Position) (buffer.Position, bool) {
	if p.Col > 0 {
		return buffer.Position{Line: p.Line, Col: p.Col - 1}, true
	}
	if p.Line > 0 {
		return buffer.Position{Line: p.Line - 1, Col: b.RuneLen(p.Line - 1)}, true
	}
	return p, false
}

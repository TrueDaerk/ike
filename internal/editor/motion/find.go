package motion

import "ike/internal/editor/buffer"

// FindKind selects one of the four in-line character searches.
type FindKind int

const (
	FindForward  FindKind = iota // f: onto the next occurrence
	TillForward                  // t: just before the next occurrence
	FindBackward                 // F: onto the previous occurrence
	TillBackward                 // T: just after the previous occurrence
)

// Find is an in-line character search, retained by the editor so ";" and ","
// can repeat or reverse it.
type Find struct {
	Kind FindKind
	Char rune
}

// Apply runs the search from p over count occurrences within the current line.
// ok is false when the character is not found (the cursor must not move). f/t
// are inclusive; F/T are exclusive (matching how operators treat them).
func (f Find) Apply(b *buffer.Buffer, p buffer.Position, count int) (Result, bool) {
	line := []rune(b.Line(p.Line))
	col := p.Col
	n := max1(count)
	switch f.Kind {
	case FindForward, TillForward:
		start := col + 1
		if f.Kind == TillForward {
			// Skip an adjacent match so a repeated "t" makes progress.
			start = col + 1
		}
		idx := start
		for hit := 0; idx < len(line); idx++ {
			if line[idx] == f.Char {
				hit++
				if hit == n {
					if f.Kind == TillForward {
						idx--
					}
					return Result{Pos: buffer.Position{Line: p.Line, Col: idx}, Kind: Inclusive}, true
				}
			}
		}
	case FindBackward, TillBackward:
		idx := col - 1
		for hit := 0; idx >= 0; idx-- {
			if line[idx] == f.Char {
				hit++
				if hit == n {
					if f.Kind == TillBackward {
						idx++
					}
					return Result{Pos: buffer.Position{Line: p.Line, Col: idx}, Kind: Exclusive}, true
				}
			}
		}
	}
	return Result{Pos: p}, false
}

// Repeat returns the find to run for ";".
func (f Find) Repeat() Find { return f }

// Reverse returns the find to run for ",", flipping search direction.
func (f Find) Reverse() Find {
	switch f.Kind {
	case FindForward:
		return Find{FindBackward, f.Char}
	case FindBackward:
		return Find{FindForward, f.Char}
	case TillForward:
		return Find{TillBackward, f.Char}
	default:
		return Find{TillForward, f.Char}
	}
}

// Valid reports whether f holds a real search (a non-zero char), so the editor
// can ignore ";"/"," before any f/t was used.
func (f Find) Valid() bool { return f.Char != 0 }

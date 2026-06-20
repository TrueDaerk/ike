package operator

import (
	"ike/internal/editor/buffer"
	"ike/internal/editor/motion"
)

// Compose turns an operator's motion into the Target it acts on, applying the
// motion's Kind: linewise spans whole lines; inclusive motions extend the range
// by one rune so the target character is operated on; exclusive motions leave
// the half-open range as-is. from is the cursor before the motion and target is
// the motion result.
func Compose(b *buffer.Buffer, from, target buffer.Position, kind motion.Kind) Target {
	switch kind {
	case motion.Linewise:
		return LineTarget(from.Line, target.Line)
	case motion.Inclusive:
		lo, hi := order(from, target)
		return CharTarget(buffer.Range{Start: lo, End: bumpRune(b, hi)})
	default: // Exclusive
		lo, hi := order(from, target)
		return CharTarget(buffer.Range{Start: lo, End: hi})
	}
}

// order returns a and b sorted into (low, high) reading order.
func order(a, b buffer.Position) (buffer.Position, buffer.Position) {
	if b.Before(a) {
		return b, a
	}
	return a, b
}

// bumpRune advances p one rune to make an inclusive endpoint half-open, crossing
// to the next line when p sits at a line's end.
func bumpRune(b *buffer.Buffer, p buffer.Position) buffer.Position {
	if p.Col < b.RuneLen(p.Line) {
		return buffer.Position{Line: p.Line, Col: p.Col + 1}
	}
	if p.Line < b.LineCount()-1 {
		return buffer.Position{Line: p.Line + 1, Col: 0}
	}
	return p
}

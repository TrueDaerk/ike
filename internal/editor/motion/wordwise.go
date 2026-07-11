package motion

import "ike/internal/editor/buffer"

// WordForward moves to the start of the next word ("w"). WordForwardBig ("W")
// treats every run of non-blank as one word.
func WordForward(b *buffer.Buffer, from buffer.Position, count int) Result {
	return wordForward(b, from, count, false)
}

// WordForwardBig is the WORD variant of WordForward.
func WordForwardBig(b *buffer.Buffer, from buffer.Position, count int) Result {
	return wordForward(b, from, count, true)
}

func wordForward(b *buffer.Buffer, from buffer.Position, count int, big bool) Result {
	p := from
	for i := 0; i < max1(count); i++ {
		start := p
		// Advance past the current run of same non-blank class.
		for c := classAt(b, p, big); c != clsBlank && classAt(b, p, big) == c; {
			q, ok := next(b, p)
			if !ok {
				break
			}
			p = q
		}
		// Skip blanks (including line breaks) to the next word start. An empty
		// line is itself a word in vim, so stop on one — unless it is where
		// this step began.
		for classAt(b, p, big) == clsBlank {
			if p != start && b.RuneLen(p.Line) == 0 {
				break
			}
			q, ok := next(b, p)
			if !ok {
				break
			}
			p = q
		}
	}
	// At the very end of the buffer there is no next word: stay on the last rune
	// rather than the newline slot (vim's behaviour).
	if p.Line == b.LineCount()-1 && p.Col >= b.RuneLen(p.Line) {
		p = b.ClampCursor(p)
	}
	return Result{Pos: p, Kind: Exclusive}
}

// WordForwardInLine is WordForward clamped to the starting line (#303,
// opt/alt+right): past the last word it lands on the line-end slot instead of
// crossing into the next line. Punctuation such as '.' forms its own class, so
// dotted identifiers (config.editor.tabWidth) yield sub-word stops for free.
func WordForwardInLine(b *buffer.Buffer, from buffer.Position, count int) Result {
	res := wordForward(b, from, count, false)
	if res.Pos.Line != from.Line {
		res.Pos = buffer.Position{Line: from.Line, Col: b.RuneLen(from.Line)}
	}
	return res
}

// WordBackwardInLine is WordBackward clamped to the starting line (#303,
// opt/alt+left): before the first word it lands on column 0 instead of
// crossing into the previous line.
func WordBackwardInLine(b *buffer.Buffer, from buffer.Position, count int) Result {
	res := wordBackward(b, from, count, false)
	if res.Pos.Line != from.Line {
		res.Pos = buffer.Position{Line: from.Line, Col: 0}
	}
	return res
}

// WordEnd moves to the end of the next word ("e"); inclusive.
func WordEnd(b *buffer.Buffer, from buffer.Position, count int) Result {
	return wordEnd(b, from, count, false)
}

// WordEndBig is the WORD variant of WordEnd ("E").
func WordEndBig(b *buffer.Buffer, from buffer.Position, count int) Result {
	return wordEnd(b, from, count, true)
}

func wordEnd(b *buffer.Buffer, from buffer.Position, count int, big bool) Result {
	p := from
	for i := 0; i < max1(count); i++ {
		// Always move at least one position forward.
		if q, ok := next(b, p); ok {
			p = q
		}
		// Skip blanks onto the next word.
		for classAt(b, p, big) == clsBlank {
			q, ok := next(b, p)
			if !ok {
				break
			}
			p = q
		}
		// Advance to the last rune of this word's run.
		c := classAt(b, p, big)
		for c != clsBlank {
			q, ok := next(b, p)
			if !ok || classAt(b, q, big) != c {
				break
			}
			p = q
		}
	}
	return Result{Pos: p, Kind: Inclusive}
}

// WordBackward moves to the start of the previous word ("b"); exclusive.
func WordBackward(b *buffer.Buffer, from buffer.Position, count int) Result {
	return wordBackward(b, from, count, false)
}

// WordBackwardBig is the WORD variant of WordBackward ("B").
func WordBackwardBig(b *buffer.Buffer, from buffer.Position, count int) Result {
	return wordBackward(b, from, count, true)
}

func wordBackward(b *buffer.Buffer, from buffer.Position, count int, big bool) Result {
	p := from
	for i := 0; i < max1(count); i++ {
		// Step back at least one.
		if q, ok := prev(b, p); ok {
			p = q
		}
		// Skip blanks backward. An empty line is itself a word in vim, so
		// stop on one.
		for classAt(b, p, big) == clsBlank {
			if b.RuneLen(p.Line) == 0 {
				break
			}
			q, ok := prev(b, p)
			if !ok {
				break
			}
			p = q
		}
		// Walk to the start of this word's run.
		c := classAt(b, p, big)
		for c != clsBlank {
			q, ok := prev(b, p)
			if !ok || classAt(b, q, big) != c {
				break
			}
			p = q
		}
	}
	return Result{Pos: p, Kind: Exclusive}
}

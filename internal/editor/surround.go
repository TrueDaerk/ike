package editor

import (
	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/operator"
	"ike/internal/editor/textobject"
)

// Surround operations (#1475), vim-surround style: ys{motion}{pair} adds a
// pair around a motion or text object, cs{old}{new} swaps the nearest
// enclosing pair, ds{old} removes it, and S{pair} wraps a visual selection.
// The opening member of a bracket pair surrounds with inner padding
// ("( x )"), the closing member without ("(x)"); on delete/change the
// opening member also strips that padding — the vim-surround convention.

// surroundResolve resolves the span ys wraps at a caret position. It is kept
// as a closure so multi-caret fan-out and "." repeat re-resolve the motion or
// text object at each new position.
type surroundResolve func(mm *Model, pos buffer.Position) (operator.Target, bool)

// surroundPair maps a surround char to the strings inserted before and after
// the target. ok is false for chars that are not a supported pair.
func surroundPair(ch rune) (open, close string, ok bool) {
	switch ch {
	case '\'', '"', '`':
		s := string(ch)
		return s, s, true
	}
	o, c, ok := textobject.CloseFor(ch)
	if !ok {
		return "", "", false
	}
	if ch == o {
		return string(o) + " ", " " + string(c), true
	}
	return string(o), string(c), true
}

// surroundCharRange converts a resolved target into the charwise span the
// delimiters go around: linewise targets (yss, visual line S) wrap from the
// first non-blank of the first line to the end of the last.
func surroundCharRange(b *buffer.Buffer, t operator.Target) buffer.Range {
	if !t.Linewise {
		return t.Range
	}
	a, z := t.Range.Start.Line, t.Range.End.Line
	if z > b.LineCount()-1 {
		z = b.LineCount() - 1
	}
	start := buffer.Position{Line: a, Col: firstNonBlank(b.Line(a))}
	return buffer.Range{Start: start, End: buffer.Position{Line: z, Col: b.RuneLen(z)}}
}

// firstNonBlank returns the rune column of the first non-blank in line, or 0
// for a blank line.
func firstNonBlank(line string) int {
	for i, r := range []rune(line) {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return 0
}

// surroundAdd wraps the span resolve yields with the pair named by ch — per
// caret as one undo unit (#145) — and records the dot so "." re-resolves at
// the new cursor.
func (m *Model) surroundAdd(resolve surroundResolve, ch rune) {
	open, close, ok := surroundPair(ch)
	if !ok {
		return
	}
	m.fanMutate(func(rec *history.Recorder, pos, _ buffer.Position) buffer.Position {
		t, ok := resolve(m, pos)
		if !ok {
			return pos
		}
		r := surroundCharRange(m.buf, t)
		rec.Apply(buffer.Insert(r.End, close))
		rec.Apply(buffer.Insert(r.Start, open))
		return r.Start
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.surroundAdd(resolve, ch) }}
}

// surroundVisual wraps the current selection with the pair named by ch and
// leaves visual mode; the dot replays it as a wrap of the same span resolved
// from the cursor.
func (m *Model) surroundVisual(ch rune) {
	target := m.visualSelection()
	m.mode = Normal
	m.pending.Reset()
	resolve := func(mm *Model, pos buffer.Position) (operator.Target, bool) { return target, true }
	open, close, ok := surroundPair(ch)
	if !ok {
		return
	}
	r := surroundCharRange(m.buf, target)
	m.mutate(func(rec *history.Recorder) buffer.Position {
		rec.Apply(buffer.Insert(r.End, close))
		rec.Apply(buffer.Insert(r.Start, open))
		return r.Start
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.surroundAdd(resolve, ch) }}
}

// surroundDelims locates the enclosing pair named by old around pos: the
// delimiter spans ready for removal. When old is the opening member of a
// bracket pair, same-line whitespace padding inside the delimiters joins the
// spans (vim-surround's "ds(" undoes what "ys...(" added).
func surroundDelims(b *buffer.Buffer, pos buffer.Position, old rune) (openR, closeR buffer.Range, ok bool) {
	var ar, in textobject.Result
	pad := false
	switch old {
	case '\'', '"', '`':
		ar = textobject.Quote(b, pos, old, true)
		in = textobject.Quote(b, pos, old, false)
	default:
		o, c, isPair := textobject.CloseFor(old)
		if !isPair {
			return openR, closeR, false
		}
		ar = textobject.Pair(b, pos, o, c, true)
		in = textobject.Pair(b, pos, o, c, false)
		pad = old == o
	}
	if !ar.OK || !in.OK {
		return openR, closeR, false
	}
	openR = buffer.Range{Start: ar.Range.Start, End: in.Range.Start}
	closeR = buffer.Range{Start: in.Range.End, End: ar.Range.End}
	if pad {
		openR.End = padForward(b, openR.End)
		closeR.Start = padBackward(b, closeR.Start, openR.End)
	}
	return openR, closeR, true
}

// padForward extends p over same-line spaces and tabs to its right.
func padForward(b *buffer.Buffer, p buffer.Position) buffer.Position {
	line := []rune(b.Line(p.Line))
	for p.Col < len(line) && (line[p.Col] == ' ' || line[p.Col] == '\t') {
		p.Col++
	}
	return p
}

// padBackward extends p over same-line spaces and tabs to its left, never
// crossing floor (the end of the opening delimiter's span).
func padBackward(b *buffer.Buffer, p, floor buffer.Position) buffer.Position {
	line := []rune(b.Line(p.Line))
	for p.Col > 0 && p.Col-1 < len(line) && (line[p.Col-1] == ' ' || line[p.Col-1] == '\t') {
		if p.Line == floor.Line && p.Col-1 < floor.Col {
			break
		}
		p.Col--
	}
	return p
}

// surroundDelete removes the enclosing pair named by old — per caret as one
// undo unit — recording the dot.
func (m *Model) surroundDelete(old rune) {
	m.fanMutate(func(rec *history.Recorder, pos, _ buffer.Position) buffer.Position {
		openR, closeR, ok := surroundDelims(m.buf, pos, old)
		if !ok {
			return pos
		}
		rec.Apply(buffer.Delete(closeR))
		rec.Apply(buffer.Delete(openR))
		return openR.Start
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.surroundDelete(old) }}
}

// surroundChange replaces the enclosing pair named by old with the pair named
// by new — per caret as one undo unit — recording the dot. The edits run back
// to front so the earlier span's positions stay valid.
func (m *Model) surroundChange(old, new rune) {
	open, close, ok := surroundPair(new)
	if !ok {
		return
	}
	m.fanMutate(func(rec *history.Recorder, pos, _ buffer.Position) buffer.Position {
		openR, closeR, ok := surroundDelims(m.buf, pos, old)
		if !ok {
			return pos
		}
		rec.Apply(buffer.Delete(closeR))
		rec.Apply(buffer.Insert(closeR.Start, close))
		rec.Apply(buffer.Delete(openR))
		rec.Apply(buffer.Insert(openR.Start, open))
		return openR.Start
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.surroundChange(old, new) }}
}

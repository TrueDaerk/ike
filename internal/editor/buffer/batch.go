package buffer

import (
	"strings"
	"unicode/utf8"
)

// ApplyAll performs edits as if Apply were called on each in order and returns
// the inverse of each, in the same order. When the edits are ordered
// bottom-up (every edit ends at or before the start of the one preceding it),
// every position lies inside the buffer and no range is reversed — the shape
// of a sorted LSP formatting result — the whole batch is applied in one pass:
// every touched line is rebuilt once, so the cost is linear in the buffer plus
// the edit text no matter how many edits share a line. Any other batch falls
// back to the per-edit Apply, which keeps its clamping semantics exactly.
func (b *Buffer) ApplyAll(edits []Edit) []Edit {
	if len(edits) > 1 {
		if inv, ok := b.applyOnePass(edits); ok {
			return inv
		}
	}
	inv := make([]Edit, len(edits))
	for i, e := range edits {
		inv[i], _ = b.Apply(e)
	}
	return inv
}

// applyOnePass walks the buffer top-down once, copying the unchanged runs and
// the replacement texts into a fresh line slice, and installs it only when the
// batch qualifies (see ApplyAll); ok false leaves the buffer untouched.
func (b *Buffer) applyOnePass(edits []Edit) (inverses []Edit, ok bool) {
	w := onePass{src: b.lines, out: make([]string, 0, len(b.lines))}
	inverses = make([]Edit, len(edits))
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		s, end := e.Range.Start, e.Range.End
		if end.Before(s) || s.Before(w.pos) || s.Col < 0 || end.Col < 0 {
			return nil, false
		}
		if !w.copyTo(s) {
			return nil, false
		}
		sb := w.byteOff
		if !w.skipTo(end) {
			return nil, false
		}
		old := w.slice(s, sb, end, w.byteOff)
		w.write(e.Text)
		inverses[i] = Edit{Range: Range{Start: s, End: textEnd(s, e.Text)}, Text: old}
	}
	w.finish()
	b.setLines(w.out)
	return inverses, true
}

// onePass is the cursor state of applyOnePass: pos/byteOff is the read
// position in src, cur the output line under construction.
type onePass struct {
	src     []string
	out     []string
	cur     strings.Builder
	pos     Position
	byteOff int
}

// copyTo advances the read position to p, copying the text passed over into
// the output. Whole untouched lines are reused without copying.
func (w *onePass) copyTo(p Position) bool {
	if p.Line >= len(w.src) {
		return false
	}
	for w.pos.Line < p.Line {
		line := w.src[w.pos.Line]
		if w.byteOff == 0 && w.cur.Len() == 0 {
			w.out = append(w.out, line)
		} else {
			w.cur.WriteString(line[w.byteOff:])
			w.flush()
		}
		w.nextLine()
	}
	from := w.byteOff
	if !w.advanceCol(p.Col) {
		return false
	}
	w.cur.WriteString(w.src[w.pos.Line][from:w.byteOff])
	return true
}

// skipTo advances the read position to p without copying (the replaced range).
func (w *onePass) skipTo(p Position) bool {
	if p.Line >= len(w.src) {
		return false
	}
	for w.pos.Line < p.Line {
		w.nextLine()
	}
	return w.advanceCol(p.Col)
}

// advanceCol moves the read position along the current line to rune column
// col; false when the line is shorter.
func (w *onePass) advanceCol(col int) bool {
	line := w.src[w.pos.Line]
	for w.pos.Col < col {
		if w.byteOff >= len(line) {
			return false
		}
		_, size := utf8.DecodeRuneInString(line[w.byteOff:])
		w.byteOff += size
		w.pos.Col++
	}
	return true
}

func (w *onePass) nextLine() {
	w.pos.Line++
	w.pos.Col, w.byteOff = 0, 0
}

// flush ends the output line under construction.
func (w *onePass) flush() {
	w.out = append(w.out, w.cur.String())
	w.cur.Reset()
}

// write emits replacement text, breaking output lines at its newlines.
func (w *onePass) write(text string) {
	for {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			w.cur.WriteString(text)
			return
		}
		w.cur.WriteString(text[:i])
		w.flush()
		text = text[i+1:]
	}
}

// slice returns the original text between s (at byte sb) and end (at byte eb).
func (w *onePass) slice(s Position, sb int, end Position, eb int) string {
	if s.Line == end.Line {
		return w.src[s.Line][sb:eb]
	}
	var b strings.Builder
	b.WriteString(w.src[s.Line][sb:])
	for l := s.Line + 1; l < end.Line; l++ {
		b.WriteByte('\n')
		b.WriteString(w.src[l])
	}
	b.WriteByte('\n')
	b.WriteString(w.src[end.Line][:eb])
	return b.String()
}

// finish copies the rest of the source after the last edit.
func (w *onePass) finish() {
	w.cur.WriteString(w.src[w.pos.Line][w.byteOff:])
	w.flush()
	w.out = append(w.out, w.src[w.pos.Line+1:]...)
}

// textEnd is the position just past text inserted at start.
func textEnd(start Position, text string) Position {
	nl := strings.Count(text, "\n")
	if nl == 0 {
		return Position{Line: start.Line, Col: start.Col + utf8.RuneCountInString(text)}
	}
	last := text[strings.LastIndexByte(text, '\n')+1:]
	return Position{Line: start.Line + nl, Col: utf8.RuneCountInString(last)}
}

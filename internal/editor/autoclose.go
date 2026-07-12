package editor

import (
	"ike/internal/editor/buffer"
)

// autoclose.go implements auto-closing bracket pairs in insert mode (#517):
// typing an opener inserts its closer with the cursor between, typing a closer
// that already sits at the cursor skips over it, and backspacing the opener of
// an empty pair removes both runes. Everything is gated on the
// editor.auto_close_pairs setting and applies per caret (#145).

// closePairs maps the auto-closing openers to their closers. Quotes and angle
// brackets are deliberately excluded — too ambiguous (apostrophes, comparison
// operators) for a plain-text heuristic.
var closePairs = map[rune]rune{'(': ')', '[': ']', '{': '}'}

// isCloser reports whether ch is the closing member of an auto-close pair.
func isCloser(ch rune) bool {
	for _, c := range closePairs {
		if c == ch {
			return true
		}
	}
	return false
}

// runeAt returns the rune at pos, or 0 at/past the line end.
func (m *Model) runeAt(pos buffer.Position) rune {
	line := []rune(m.buf.Line(pos.Line))
	if pos.Col < 0 || pos.Col >= len(line) {
		return 0
	}
	return line[pos.Col]
}

// shouldClose reports whether auto-inserting a closer at pos is sensible: the
// cursor sits at the line end, before whitespace, or before another closer.
// Directly before other text the opener inserts alone.
func (m *Model) shouldClose(pos buffer.Position) bool {
	switch m.runeAt(pos) {
	case 0, ' ', '\t', ')', ']', '}':
		return true
	}
	return false
}

// autoCloseWrite intercepts a single typed rune for pair handling, returning
// false when the rune is not pair-relevant (or no caret benefits) so the plain
// insert path proceeds. Each caret decides on its own context, so one fan-out
// can mix pairing, plain insert, and skip-over.
func (m *Model) autoCloseWrite(text string) bool {
	r := []rune(text)
	if len(r) != 1 {
		return false
	}
	if closer, ok := closePairs[r[0]]; ok {
		m.autoCloseOpen(r[0], closer)
		return true
	}
	if isCloser(r[0]) {
		return m.autoCloseSkip(r[0])
	}
	return false
}

// autoCloseOpen inserts open at every caret, appending closer where the caret's
// context allows it (shouldClose) and leaving the caret between the pair.
func (m *Model) autoCloseOpen(open, closer rune) {
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	m.fanApply(func(pos, _ buffer.Position) buffer.Position {
		if !m.shouldClose(pos) {
			return m.insert.rec.Apply(buffer.Insert(pos, string(open)))
		}
		end := m.insert.rec.Apply(buffer.Insert(pos, string(open)+string(closer)))
		return buffer.Position{Line: end.Line, Col: end.Col - 1}
	})
	// "." replays only the keystroke: the closer is usually typed (and
	// skipped over) later, which appends it below, so a full "(x)" run
	// replays exactly; an unclosed insert replays without the closer.
	m.insert.typed += string(open)
	m.dirtyFromInsert()
}

// autoCloseSkip moves carets sitting on ch past it instead of inserting a
// duplicate closer. Carets not on ch still insert it. Returns false when no
// caret sits on ch, deferring to the plain insert path.
func (m *Model) autoCloseSkip(ch rune) bool {
	hit := m.runeAt(m.cursor) == ch
	for _, c := range m.carets {
		hit = hit || m.runeAt(c.pos) == ch
	}
	if !hit {
		return false
	}
	edited := false
	m.fanApply(func(pos, _ buffer.Position) buffer.Position {
		if m.runeAt(pos) == ch {
			return buffer.Position{Line: pos.Line, Col: pos.Col + 1}
		}
		if m.insert.rec == nil {
			m.insert.rec = m.newRecorder()
		}
		edited = true
		return m.insert.rec.Apply(buffer.Insert(pos, string(ch)))
	})
	m.insert.typed += string(ch)
	if edited {
		m.dirtyFromInsert()
	} else {
		m.emit(EventCursorMove)
	}
	return true
}

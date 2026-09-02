package editor

import (
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/motion"
	"ike/internal/editor/operator"
)

// lineops.go holds the JetBrains line-editing and document-navigation
// commands the #2400 keybind audit found missing: move line up/down, delete
// line, delete word backward, document start/end and the select-to-line-edge
// pair. Every one of them existed only as a vim gesture (or, for the two
// kills, only inside insert mode), so a user coming from JetBrains pressed
// the chord, nothing happened, and the keymap layer logged it as unbound.
//
// The commands are selection-aware where JetBrains is: a command that acts on
// "the line" acts on every line the selection touches.

// lineSpan returns the line range a line-wise command acts on: the lines the
// visual selection touches, or the cursor line when there is no selection.
func (m Model) lineSpan() (first, last int) {
	first, last = m.cursor.Line, m.cursor.Line
	if m.mode.IsVisual() {
		first, last = m.anchor.Line, m.cursor.Line
		if last < first {
			first, last = last, first
		}
	}
	return first, last
}

// moveLines moves the touched lines one line up (dir -1) or down (dir +1),
// swapping them with the neighbouring line — JetBrains' Move Line Up/Down.
// The cursor keeps its column and rides along, and a visual selection keeps
// covering the same text, so the chord repeats. The swap is one edit (hence
// one undo step): the block plus its neighbour is rewritten in the new order.
func (m *Model) moveLines(dir int) {
	if m.insert.active {
		m.commitInsert()
	}
	first, last := m.lineSpan()
	if dir < 0 && first == 0 {
		return
	}
	if dir > 0 && last >= m.buf.LineCount()-1 {
		return
	}

	block := make([]string, 0, last-first+2)
	for i := first; i <= last; i++ {
		block = append(block, m.buf.Line(i))
	}
	var span buffer.Range
	var lines []string
	if dir < 0 {
		span = buffer.Range{
			Start: buffer.Position{Line: first - 1, Col: 0},
			End:   buffer.Position{Line: last, Col: m.buf.RuneLen(last)},
		}
		lines = append(block, m.buf.Line(first-1))
	} else {
		span = buffer.Range{
			Start: buffer.Position{Line: first, Col: 0},
			End:   buffer.Position{Line: last + 1, Col: m.buf.RuneLen(last + 1)},
		}
		lines = append([]string{m.buf.Line(last + 1)}, block...)
	}

	text := strings.Join(lines, "\n")
	col, cursorLine, anchorLine := m.cursor.Col, m.cursor.Line+dir, m.anchor.Line+dir
	m.mutate(func(rec *history.Recorder) buffer.Position {
		rec.Apply(buffer.Edit{Range: span, Text: text})
		return buffer.Position{Line: cursorLine, Col: col}
	})
	if m.mode.IsVisual() {
		m.anchor = m.buf.Clamp(buffer.Position{Line: anchorLine, Col: m.anchor.Col})
	}
	m.dot = &dotCommand{run: func(mm *Model) { mm.moveLines(dir) }}
	m.scroll()
}

// deleteLines removes the current line — or every line the selection touches
// — and leaves the cursor in the same column of the line that moved up into
// its place (JetBrains' Delete Line on cmd+backspace). Mid-insert it defers
// to the session's own line kill (#955) so the deletion joins the open undo
// unit instead of committing the insert first. A closed fold under the cursor
// goes whole, like every other linewise operator (#1741).
func (m *Model) deleteLines() {
	if m.insert.active {
		m.insertKillLine()
		m.scroll()
		return
	}
	first, last := m.lineSpan()
	col := m.cursor.Col
	if m.mode.IsVisual() {
		m.mode = Normal
		m.shiftSelect = false
	}
	reg := m.pending.Register
	m.pending.Reset()
	m.runOperator('d', operator.LineTarget(first, last), reg)
	m.cursor = m.buf.ClampCursor(buffer.Position{Line: m.cursor.Line, Col: col})
	m.desiredCol = m.cursor.Col
	m.dot = &dotCommand{run: func(mm *Model) { mm.deleteLines() }}
	m.emit(EventCursorMove)
	m.scroll()
}

// deleteWordBackward deletes the word before the cursor — the everywhere
// convention on alt+backspace (#246), here as a bound command so it works
// outside insert mode too. Mid-insert it runs the session's own kill, so the
// deletion joins the open undo unit; otherwise it is the `db` operator.
func (m *Model) deleteWordBackward() {
	if m.insert.active {
		m.insertKillBack(func(pos buffer.Position) buffer.Position {
			return motion.WordBackward(m.buf, pos, 1).Pos
		})
		m.scroll()
		return
	}
	m.killWord(false, 1)
	m.scroll()
}

// docEdge moves the cursor to the first (end=false) or last line of the
// buffer — the `gg` / `G` motions as bound commands (cmd/ctrl+home / +end).
// Both are jumps, so the departure point lands in the navigation history, and
// scroll() opens a collapsed fold the target line hides in. In a visual mode
// the motion extends the selection, exactly like the vim keys do.
func (m *Model) docEdge(end bool) {
	if m.insert.active {
		m.commitInsert()
	}
	res := motion.First(m.buf, m.cursor, 0)
	if end {
		res = motion.Last(m.buf, m.cursor, 0)
	}
	res.Jump = true
	m.applyMotionOrOperator(res, 1)
	m.scroll()
}

// selectToLineEdge extends (or starts) a selection to the line start
// (end=false) or line end — shift+home / shift+end as commands. It mirrors
// the shift+arrow path in normal mode: without a selection it enters a
// shift-select charwise visual anchored at the cursor, then applies the plain
// motion, so an unshifted key afterwards drops the selection like vim's
// keymodel=stopsel (#326).
func (m *Model) selectToLineEdge(end bool) {
	if m.insert.active {
		m.commitInsert()
	}
	if !m.mode.IsVisual() {
		m.enterVisual(Visual)
		m.shiftSelect = true
	}
	key := "home"
	if end {
		key = "end"
	}
	if res, ok := m.resolveMotion(key, 0, 1); ok {
		m.applyMotionOrOperator(res, 1)
	}
	m.scroll()
}

package editor

import (
	tea "github.com/charmbracelet/bubbletea"

	"ike/internal/editor/buffer"
)

// updateInsert handles a key in insert or replace mode, applying every edit
// through the open insert-session recorder so the whole insert is one undo unit.
func (m *Model) updateInsert(key tea.KeyMsg) {
	switch key.Type {
	case tea.KeyEsc:
		m.commitInsert()
	case tea.KeyEnter:
		indent := ""
		if m.autoIndent {
			indent = m.indentOf(m.cursor.Line)
		}
		m.insertText("\n" + indent)
	case tea.KeyBackspace, tea.KeyCtrlH:
		m.insertBackspace()
	case tea.KeyTab:
		m.insertText(m.tabText())
	case tea.KeyLeft:
		m.insertMove(0, -1)
	case tea.KeyRight:
		m.insertMove(0, 1)
	case tea.KeyUp:
		m.insertMove(-1, 0)
	case tea.KeyDown:
		m.insertMove(1, 0)
	case tea.KeyHome:
		m.cursor = buffer.Position{Line: m.cursor.Line, Col: 0}
		m.desiredCol = 0
	case tea.KeyEnd:
		m.cursor = buffer.Position{Line: m.cursor.Line, Col: m.buf.RuneLen(m.cursor.Line)}
		m.desiredCol = m.cursor.Col
	case tea.KeySpace:
		m.writeRunes(" ")
	case tea.KeyRunes:
		m.writeRunes(string(key.Runes))
	default:
		// Shift+arrows (word nav), PgUp/PgDn and Ctrl motions also work mid-insert.
		if res, ok := m.resolveMotion(key.String(), 0, 1); ok {
			m.cursor = m.buf.Clamp(res.Pos)
			m.desiredCol = m.cursor.Col
			m.emit(EventCursorMove)
		}
	}
}

// insertMove nudges the cursor in insert mode, allowing the one-past-end column
// (so typing can continue at the line end) rather than the normal-mode clamp.
func (m *Model) insertMove(dLine, dCol int) {
	p := buffer.Position{Line: m.cursor.Line + dLine, Col: m.cursor.Col + dCol}
	m.cursor = m.buf.Clamp(p)
	m.desiredCol = m.cursor.Col
	m.emit(EventCursorMove)
}

// writeRunes inserts text, overwriting in replace mode and triggering a
// completion event after a ".".
func (m *Model) writeRunes(text string) {
	if m.mode == Replace {
		m.replaceText(text)
		return
	}
	m.insertText(text)
	if text == "." {
		m.emit(EventCompletionTrigger)
	}
}

// insertText splices text at the cursor through the session recorder.
func (m *Model) insertText(text string) {
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	end := m.insert.rec.Apply(buffer.Insert(m.cursor, text))
	m.cursor = end
	m.desiredCol = end.Col
	m.insert.typed += text
	m.dirtyFromInsert()
}

// replaceText overwrites the runes under the cursor (R mode), inserting past the
// line end.
func (m *Model) replaceText(text string) {
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	for _, r := range text {
		end := m.cursor
		if m.cursor.Col < m.buf.RuneLen(m.cursor.Line) {
			end.Col++ // overwrite the existing rune
		}
		m.cursor = m.insert.rec.Apply(buffer.Edit{Range: buffer.Range{Start: m.cursor, End: end}, Text: string(r)})
	}
	m.desiredCol = m.cursor.Col
	m.insert.typed += text
	m.dirtyFromInsert()
}

// insertBackspace deletes the rune before the cursor, joining lines at column 0.
func (m *Model) insertBackspace() {
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	switch {
	case m.cursor.Col > 0:
		start := buffer.Position{Line: m.cursor.Line, Col: m.cursor.Col - 1}
		m.insert.rec.Apply(buffer.Delete(buffer.Range{Start: start, End: m.cursor}))
		m.cursor = start
	case m.cursor.Line > 0:
		prevLen := m.buf.RuneLen(m.cursor.Line - 1)
		start := buffer.Position{Line: m.cursor.Line - 1, Col: prevLen}
		m.insert.rec.Apply(buffer.Delete(buffer.Range{Start: start, End: m.cursor}))
		m.cursor = start
	default:
		return
	}
	m.desiredCol = m.cursor.Col
	// Backspace approximately rewinds the recorded text for "." replay.
	if r := []rune(m.insert.typed); len(r) > 0 {
		m.insert.typed = string(r[:len(r)-1])
	}
	m.dirtyFromInsert()
}

// dirtyFromInsert marks the buffer dirty and emits a change while typing.
func (m *Model) dirtyFromInsert() {
	m.dirty = true
	m.emit(EventChange)
}

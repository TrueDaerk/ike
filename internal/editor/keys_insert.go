package editor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/motion"
)

// updateInsert handles a key in insert or replace mode, applying every edit
// through the open insert-session recorder so the whole insert is one undo unit.
func (m *Model) updateInsert(key tea.KeyPressMsg) {
	// While the completion popup is open it intercepts navigation/accept keys
	// first; anything else (typing, backspace) falls through to normal insert
	// handling and then re-filters the list below.
	if m.comp != nil {
		if m.completionKey(key) {
			return
		}
	}
	switch {
	case key.Code == tea.KeyEscape:
		m.commitInsert()
	// ctrl+space is the conventional manual completion trigger (#302). It
	// arrives as ctrl+' ' under the Kitty protocol and as ctrl+@ (NUL) from
	// legacy terminals; both request completion at the cursor through the
	// same event the "." auto-trigger uses. With the popup already open the
	// re-emit re-queries the server.
	case (key.Code == ' ' || key.Code == '@' || key.Code == tea.KeySpace) && key.Mod == tea.ModCtrl:
		m.emit(EventCompletionTrigger)
	case key.Code == tea.KeyEnter:
		indent := ""
		if m.autoIndent {
			// Smart indent (Roadmap 0260) keys off what stays on the line: a
			// mid-line split indents by the text left of the cursor.
			left := []rune(m.buf.Line(m.cursor.Line))
			col := min(m.cursor.Col, len(left))
			indent = m.smartIndent(string(left[:col]))
		}
		m.insertText("\n" + indent)
	// Word/line kills (#246) come before the plain-backspace case, which
	// matches KeyBackspace regardless of modifiers. alt+backspace mirrors the
	// terminal pane's macOS convention (#240), ctrl+w is the vim-native twin;
	// cmd+backspace / ctrl+u kill to the line start the same way.
	case key.Code == tea.KeyBackspace && key.Mod&^tea.ModShift == tea.ModAlt,
		key.Code == 'w' && key.Mod == tea.ModCtrl:
		m.insertDeleteBack(motion.WordBackward(m.buf, m.cursor, 1).Pos)
	case key.Code == tea.KeyBackspace && (key.Mod&^tea.ModShift == tea.ModSuper || key.Mod&^tea.ModShift == tea.ModMeta),
		key.Code == 'u' && key.Mod == tea.ModCtrl:
		m.insertDeleteBack(buffer.Position{Line: m.cursor.Line, Col: 0})
	case key.Code == tea.KeyBackspace, key.Code == 'h' && key.Mod == tea.ModCtrl:
		m.insertBackspace()
	// Shift+Tab dedents the whole current line one unit (Roadmap 0260),
	// regardless of the cursor column; plain Tab inserts one unit at the cursor.
	case key.Code == tea.KeyTab && key.Mod&tea.ModShift != 0:
		m.insertDedentLine()
	case key.Code == tea.KeyTab:
		m.insertText(m.tabText())
	// Plain (or Shift-modified) arrows move the cursor; Alt/Ctrl chords fall
	// through to resolveMotion below for word/paragraph navigation.
	case key.Code == tea.KeyLeft && key.Mod&^tea.ModShift == 0:
		m.insertMove(0, -1)
	case key.Code == tea.KeyRight && key.Mod&^tea.ModShift == 0:
		m.insertMove(0, 1)
	case key.Code == tea.KeyUp && key.Mod&^tea.ModShift == 0:
		m.insertMove(-1, 0)
	case key.Code == tea.KeyDown && key.Mod&^tea.ModShift == 0:
		m.insertMove(1, 0)
	case key.Code == tea.KeyHome:
		m.cursor = buffer.Position{Line: m.cursor.Line, Col: 0}
		m.desiredCol = 0
	case key.Code == tea.KeyEnd:
		m.cursor = buffer.Position{Line: m.cursor.Line, Col: m.buf.RuneLen(m.cursor.Line)}
		m.desiredCol = m.cursor.Col
	case key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		// Printable input, including a bare space (Text == " ").
		m.writeRunes(key.Text)
	default:
		// Alt/Ctrl+arrows (word nav), PgUp/PgDn and Ctrl motions also work mid-insert.
		if res, ok := m.resolveMotion(key.String(), 0, 1); ok {
			m.cursor = m.buf.Clamp(res.Pos)
			m.desiredCol = m.cursor.Col
			m.emit(EventCursorMove)
		}
	}
	// After typing/backspace, drop the popup if nothing matches the new prefix.
	if m.comp != nil && len(m.filteredCompletion()) == 0 {
		m.comp = nil
	}
}

// completionKey handles a key while the completion popup is open, returning true
// if it consumed the key (navigation / accept / dismiss). Typing and backspace
// return false so normal insert handling proceeds and the list re-filters.
func (m *Model) completionKey(key tea.KeyPressMsg) bool {
	switch {
	case key.Code == tea.KeyDown, key.Code == 'n' && key.Mod == tea.ModCtrl:
		m.completionMove(1)
		return true
	case key.Code == tea.KeyUp, key.Code == 'p' && key.Mod == tea.ModCtrl:
		m.completionMove(-1)
		return true
	// Only a plain Tab accepts; Shift+Tab falls through to the line dedent.
	case key.Code == tea.KeyEnter, key.Code == tea.KeyTab && key.Mod&tea.ModShift == 0:
		m.completionAccept()
		return true
	case key.Code == tea.KeyEscape:
		m.completionCancel()
		return true
	}
	return false
}

// insertMove nudges the cursor in insert mode, allowing the one-past-end column
// (so typing can continue at the line end) rather than the normal-mode clamp.
func (m *Model) insertMove(dLine, dCol int) {
	// Arrow motion in insert mode emits no change event, so the popup would
	// trail the cursor instead of being retriggered/dismissed (#315).
	m.dismissSignature()
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

// insertDeleteBack deletes from start (a position at or before the cursor)
// up to the cursor through the session recorder — the shared engine behind
// the word kill (alt+backspace / ctrl+w) and the line-start kill
// (cmd+backspace / ctrl+u) in insert mode (#246). A start at or after the
// cursor is a no-op.
func (m *Model) insertDeleteBack(start buffer.Position) {
	start = m.buf.Clamp(start)
	if start.Line > m.cursor.Line || (start.Line == m.cursor.Line && start.Col >= m.cursor.Col) {
		return
	}
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	crossLine := start.Line != m.cursor.Line
	deleted := m.cursor.Col - start.Col // rune count when staying on one line
	m.insert.rec.Apply(buffer.Delete(buffer.Range{Start: start, End: m.cursor}))
	m.cursor = start
	m.desiredCol = start.Col
	// Approximately rewind the recorded text for "." replay, like
	// insertBackspace; a cross-line kill just clears it.
	if r := []rune(m.insert.typed); crossLine || deleted >= len(r) {
		m.insert.typed = ""
	} else {
		m.insert.typed = string(r[:len(r)-deleted])
	}
	m.dirtyFromInsert()
}

// insertDedentLine removes one indent unit from the start of the current line
// (Shift+Tab in insert mode, Roadmap 0260): the same unit "<<" removes — one
// leading tab, or up to tabWidth leading spaces. The whole line shifts left no
// matter where the cursor sits; the cursor follows the removed columns. A line
// with no leading whitespace is a no-op. Like insertBackspace, the recorded
// "." text is only approximate (the dedent is not replayed).
func (m *Model) insertDedentLine() {
	n := dedentCols(m.buf.Line(m.cursor.Line), m.tabWidth)
	if n == 0 {
		return
	}
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	m.insert.rec.Apply(buffer.Delete(buffer.Range{
		Start: buffer.Position{Line: m.cursor.Line, Col: 0},
		End:   buffer.Position{Line: m.cursor.Line, Col: n},
	}))
	if m.cursor.Col -= n; m.cursor.Col < 0 {
		m.cursor.Col = 0
	}
	m.desiredCol = m.cursor.Col
	m.dirtyFromInsert()
}

// dirtyFromInsert marks the buffer dirty and emits a change while typing.
func (m *Model) dirtyFromInsert() {
	m.dirty = true
	m.emit(EventChange)
}

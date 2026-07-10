package editor

import (
	"os"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/operator"
	"ike/internal/editor/search"
)

// ActionMsg requests a named editor action. It is the single path the plugin
// registry (commands.go), the palette (07) and keybindings (08) use to drive the
// editor — there is no parallel command-dispatch mechanism.
type ActionMsg struct{ Action string }

// insertSession records an in-progress insert so the whole insert commits as one
// undo unit and "." can replay it. pre recreates the structural part (the line
// opened by o/O, or the text deleted by c) when the command is repeated.
type insertSession struct {
	active bool
	rec    *history.Recorder
	typed  string // text typed so far, for "." replay
	pre    func(m *Model, rec *history.Recorder) buffer.Position
}

// dotCommand is the last change, replayed by ".".
type dotCommand struct{ run func(m *Model) }

// mutate runs a one-shot change: it opens a recorder at the cursor, lets fn apply
// edits and return the new cursor, then commits to history (when non-empty),
// marks the buffer dirty and emits a change event.
func (m *Model) mutate(fn func(rec *history.Recorder) buffer.Position) {
	rec := history.NewRecorder(m.buf, m.cursor)
	newCur := fn(rec)
	if !rec.Empty() {
		m.hist.Push(rec.Commit(newCur))
		m.dirty = true
		m.emit(EventChange)
	}
	m.cursor = m.buf.ClampCursor(newCur)
	m.desiredCol = m.cursor.Col
}

// runOperator resolves op against target and applies it, recording a dot that
// re-resolves the target from the cursor when repeated.
func (m *Model) runOperator(op rune, target operator.Target, reg rune) {
	switch op {
	case 'y':
		cur := operator.Yank(m.buf, m.regs, reg, target)
		m.cursor = cur
		m.desiredCol = cur.Col
	case 'd':
		m.mutate(func(rec *history.Recorder) buffer.Position {
			return operator.Delete(m.buf, rec, m.regs, reg, target)
		})
	case 'c':
		m.beginInsertChange(reg, target)
	case '>':
		m.indentTarget(target, 1)
	case '<':
		m.indentTarget(target, -1)
	}
}

// indentTarget shifts every line in target's range right (dir>0) or left
// (dir<0) by one shiftwidth (the configured tab unit), recording a dot.
func (m *Model) indentTarget(target operator.Target, dir int) {
	a, z := target.Range.Start.Line, target.Range.End.Line
	if z < a {
		a, z = z, a
	}
	unit := m.tabText()
	m.mutate(func(rec *history.Recorder) buffer.Position {
		for l := a; l <= z && l <= m.buf.LineCount()-1; l++ {
			if dir > 0 {
				if m.buf.Line(l) != "" {
					rec.Apply(buffer.Insert(buffer.Position{Line: l, Col: 0}, unit))
				}
			} else if n := dedentCols(m.buf.Line(l), m.tabWidth); n > 0 {
				rec.Apply(buffer.Delete(buffer.Range{Start: buffer.Position{Line: l, Col: 0}, End: buffer.Position{Line: l, Col: n}}))
			}
		}
		return buffer.Position{Line: a, Col: 0}
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.indentTarget(target, dir) }}
}

// dedentCols returns how many leading rune columns to drop for one dedent: a
// single leading tab, or up to tabWidth leading spaces.
func dedentCols(line string, tabWidth int) int {
	if strings.HasPrefix(line, "\t") {
		return 1
	}
	n := 0
	for n < len(line) && n < tabWidth && line[n] == ' ' {
		n++
	}
	return n
}

// toggleCase implements "~": swap the case of count runes from the cursor and
// advance past them.
func (m *Model) toggleCase(count int) {
	line := []rune(m.buf.Line(m.cursor.Line))
	if m.cursor.Col >= len(line) {
		return
	}
	end := m.cursor.Col + count
	if end > len(line) {
		end = len(line)
	}
	var sb strings.Builder
	for i := m.cursor.Col; i < end; i++ {
		sb.WriteRune(swapCase(line[i]))
	}
	from := m.cursor
	m.mutate(func(rec *history.Recorder) buffer.Position {
		rec.Apply(buffer.Edit{Range: buffer.Range{Start: from, End: buffer.Position{Line: from.Line, Col: end}}, Text: sb.String()})
		return buffer.Position{Line: from.Line, Col: end}
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.toggleCase(count) }}
}

// swapCase flips the case of a single rune.
func swapCase(r rune) rune {
	switch {
	case unicode.IsLower(r):
		return unicode.ToUpper(r)
	case unicode.IsUpper(r):
		return unicode.ToLower(r)
	default:
		return r
	}
}

// beginInsertChange performs the delete half of a change and enters insert mode
// with the recorder still open, so the typed text joins the same undo unit.
func (m *Model) beginInsertChange(reg rune, target operator.Target) {
	rec := history.NewRecorder(m.buf, m.cursor)
	at := operator.Change(m.buf, rec, m.regs, reg, target)
	m.cursor = m.buf.Clamp(at)
	m.startInsertWith(rec, func(mm *Model, r *history.Recorder) buffer.Position {
		// Repeat re-deletes the same target from the current cursor.
		return operator.Change(mm.buf, r, mm.regs, reg, target)
	})
}

// startInsert enters insert mode for i/a/o/O with the structural edit (if any)
// already applied to rec; pre lets "." recreate that structural edit.
func (m *Model) startInsertWith(rec *history.Recorder, pre func(*Model, *history.Recorder) buffer.Position) {
	m.mode = Insert
	m.insert = insertSession{active: true, rec: rec, pre: pre}
	m.emit(EventCursorMove)
}

// commitInsert leaves insert mode, commits the session's recorder as one change,
// and records the dot replay (structural pre + typed text).
func (m *Model) commitInsert() {
	s := m.insert
	text := s.typed
	if s.rec != nil && !s.rec.Empty() {
		m.hist.Push(s.rec.Commit(m.cursor))
		m.dirty = true
		m.emit(EventChange)
	}
	pre := s.pre
	m.dot = &dotCommand{run: func(mm *Model) {
		mm.mutate(func(rec *history.Recorder) buffer.Position {
			if pre != nil {
				mm.cursor = mm.buf.Clamp(pre(mm, rec))
			}
			if text != "" {
				end := rec.Apply(buffer.Insert(mm.cursor, text))
				mm.cursor = end
			}
			return mm.cursor
		})
	}}
	m.insert = insertSession{}
	m.mode = Normal
	if m.cursor.Col > 0 {
		m.cursor.Col--
	}
	m.cursor = m.buf.ClampCursor(m.cursor)
	m.desiredCol = m.cursor.Col
}

// repeatDot replays the last change count times.
func (m *Model) repeatDot(count int) {
	if m.dot == nil {
		return
	}
	for i := 0; i < count; i++ {
		m.dot.run(m)
	}
}

// paste inserts the register entry relative to the cursor (p/P), recording a dot.
func (m *Model) paste(reg rune, after bool, count int, gp bool) {
	e := m.regs.Get(reg)
	if e.Text == "" {
		return
	}
	m.mutate(func(rec *history.Recorder) buffer.Position {
		return operator.Paste(m.buf, rec, e, m.cursor, after, count, gp)
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.paste(reg, after, count, gp) }}
}

// deleteUnderCursor implements x: delete count runes from the cursor.
func (m *Model) deleteUnderCursor(reg rune, count int) {
	if m.buf.RuneLen(m.cursor.Line) == 0 {
		return
	}
	end := m.cursor
	end.Col += count
	if max := m.buf.RuneLen(m.cursor.Line); end.Col > max {
		end.Col = max
	}
	target := operator.CharTarget(buffer.Range{Start: m.cursor, End: end})
	m.mutate(func(rec *history.Recorder) buffer.Position {
		return operator.Delete(m.buf, rec, m.regs, reg, target)
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.deleteUnderCursor(reg, count) }}
}

// replaceChar implements r: overwrite the rune under the cursor with ch.
func (m *Model) replaceChar(ch rune, count int) {
	if m.buf.RuneLen(m.cursor.Line) < m.cursor.Col+count {
		return
	}
	end := buffer.Position{Line: m.cursor.Line, Col: m.cursor.Col + count}
	m.mutate(func(rec *history.Recorder) buffer.Position {
		rec.Apply(buffer.Edit{Range: buffer.Range{Start: m.cursor, End: end}, Text: strings.Repeat(string(ch), count)})
		return buffer.Position{Line: m.cursor.Line, Col: m.cursor.Col + count - 1}
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.replaceChar(ch, count) }}
}

// joinLines implements J: join count following lines onto the current one with a
// single separating space.
func (m *Model) joinLines(count int) {
	if count < 2 {
		count = 2
	}
	m.mutate(func(rec *history.Recorder) buffer.Position {
		landCol := m.buf.RuneLen(m.cursor.Line)
		for i := 0; i < count-1; i++ {
			line := m.cursor.Line
			if line+1 > m.buf.LineCount()-1 {
				break
			}
			joinAt := buffer.Position{Line: line, Col: m.buf.RuneLen(line)}
			// Drop leading blanks of the next line, replace the break with a space.
			trimmed := strings.TrimLeft(m.buf.Line(line+1), " \t")
			landCol = m.buf.RuneLen(line)
			rec.Apply(buffer.Edit{
				Range: buffer.Range{Start: joinAt, End: buffer.Position{Line: line + 1, Col: m.buf.RuneLen(line + 1)}},
				Text:  " " + trimmed,
			})
		}
		return buffer.Position{Line: m.cursor.Line, Col: landCol}
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.joinLines(count) }}
}

// undo reverts the last count changes and moves the cursor to the last recorded
// position, stopping early when the history runs out (vim's {count}u).
// An undo requested mid-insert (Cmd+Z while typing) first commits the open insert
// session so the whole typed run becomes one undoable unit, then reverts it —
// undo therefore behaves the same from insert and normal mode.
func (m *Model) undo(count int) {
	if m.insert.active {
		m.commitInsert()
	}
	undone := false
	for i := 0; i < count; i++ {
		cur, ok := m.hist.Undo(m.buf)
		if !ok {
			break
		}
		undone = true
		m.cursor = m.buf.ClampCursor(cur)
	}
	if undone {
		m.desiredCol = m.cursor.Col
		// An undo mutates the buffer away from what was last written, so the
		// document is dirty again — without this, undoing past a save (or an
		// auto-save, #174) would leave changes that never get persisted.
		m.dirty = true
		m.emit(EventChange)
	}
}

// redo re-applies the last count undone changes ({count}ctrl+r), stopping early
// when the redo stack runs out. Like undo, it first commits any open insert
// session so the redo stack is well defined regardless of mode.
func (m *Model) redo(count int) {
	if m.insert.active {
		m.commitInsert()
	}
	redone := false
	for i := 0; i < count; i++ {
		cur, ok := m.hist.Redo(m.buf)
		if !ok {
			break
		}
		redone = true
		m.cursor = m.buf.ClampCursor(cur)
	}
	if redone {
		m.desiredCol = m.cursor.Col
		m.dirty = true
		m.emit(EventChange)
	}
}

// save writes the buffer to disk, applying the trim-trailing-whitespace and
// final-newline policies, and clears the dirty flag. No-op without a file.
func (m *Model) save() error { return m.saveAs(m.path) }

// saveAs writes the buffer to path (":w file"). It updates the editor's path on
// success so subsequent saves target the new file.
func (m *Model) saveAs(path string) error {
	if path == "" {
		return nil
	}
	lines := m.buf.Lines()
	if m.trimTrailing {
		for i, l := range lines {
			lines[i] = strings.TrimRight(l, " \t")
		}
	}
	data := strings.Join(lines, "\n")
	if m.insertFinalNewline {
		data += "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return err
	}
	m.path = path
	m.dirty = false
	m.emit(EventSave)
	return nil
}

// runAction executes a named action requested via ActionMsg (registry commands).
func (m Model) runAction(action string) (Model, tea.Cmd) {
	switch action {
	case "write":
		if cmd := m.saveGuarded(m.path); cmd != nil {
			return m, cmd
		}
	case "quit":
		return m, func() tea.Msg { return CloseMsg{} }
	case "write_quit":
		if cmd := m.saveGuarded(m.path); cmd != nil {
			return m, cmd // conflict: prompt first, keep the pane open
		}
		return m, func() tea.Msg { return CloseMsg{} }
	case "undo":
		m.undo(1)
	case "redo":
		m.redo(1)
	case "copy":
		m.clipboardCopy()
	case "cut":
		m.clipboardCut()
	case "paste":
		m.clipboardPaste()
	case "line_start":
		m.lineStart()
	case "line_end":
		m.lineEnd()
	case "find":
		if m.insert.active {
			m.commitInsert()
		}
		m.beginSearch(search.Forward)
	case "duplicate_line":
		if m.insert.active {
			m.commitInsert()
		}
		m.duplicateLine()
	case "comment_line":
		cmd := m.commentLine()
		m.scroll()
		return m, cmd
	case "comment_block":
		cmd := m.commentBlock()
		m.scroll()
		return m, cmd
	}
	m.scroll()
	return m, nil
}

// clipboardCopy yanks the visual selection — or the current line when nothing
// is selected — into the system-clipboard register `+` (Cmd+C).
func (m *Model) clipboardCopy() {
	if m.mode.IsVisual() {
		m.visualOperateReg('y', '+')
		return
	}
	m.runOperator('y', operator.LineTarget(m.cursor.Line, m.cursor.Line), '+')
}

// clipboardCut deletes the visual selection — or the current line — into the
// system-clipboard register `+` (Cmd+X).
func (m *Model) clipboardCut() {
	if m.mode.IsVisual() {
		m.visualOperateReg('d', '+')
		return
	}
	m.runOperator('d', operator.LineTarget(m.cursor.Line, m.cursor.Line), '+')
}

// clipboardPaste inserts the system clipboard at the cursor (Cmd+V): it
// replaces the selection in visual mode and, mid-insert, splices through the
// open insert session so the paste joins the same undo unit.
func (m *Model) clipboardPaste() {
	if m.mode.IsVisual() {
		m.visualPaste('+')
		return
	}
	e := m.regs.Get('+')
	if e.Text == "" {
		return
	}
	if m.insert.active {
		m.insertText(e.Text)
		return
	}
	m.paste('+', false, 1, false)
}

// duplicateLine inserts a copy of the current line below it and moves the
// cursor onto the copy, keeping its column (JetBrains Cmd+D). Dot-repeatable.
func (m *Model) duplicateLine() {
	line := m.buf.Line(m.cursor.Line)
	at := buffer.Position{Line: m.cursor.Line, Col: m.buf.RuneLen(m.cursor.Line)}
	col := m.cursor.Col
	m.mutate(func(rec *history.Recorder) buffer.Position {
		rec.Apply(buffer.Insert(at, "\n"+line))
		return buffer.Position{Line: at.Line + 1, Col: col}
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.duplicateLine() }}
}

// lineStart moves the cursor to column 0 (Cmd+Left).
func (m *Model) lineStart() {
	m.moveTo(buffer.Position{Line: m.cursor.Line, Col: 0})
}

// lineEnd moves the cursor to the line end (Cmd+Right): one past the last rune
// while an insert session is open, on the last rune otherwise.
func (m *Model) lineEnd() {
	col := m.buf.RuneLen(m.cursor.Line)
	if m.insert.active {
		m.cursor = buffer.Position{Line: m.cursor.Line, Col: col}
		m.desiredCol = col
		m.emit(EventCursorMove)
		return
	}
	m.moveTo(buffer.Position{Line: m.cursor.Line, Col: col})
}

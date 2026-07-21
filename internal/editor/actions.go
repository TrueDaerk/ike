package editor

import (
	"os"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/operator"
	"ike/internal/editor/register"
	"ike/internal/editor/search"
	"ike/internal/textenc"
	"ike/internal/undostore"
)

// ActionMsg requests a named editor action. It is the single path the plugin
// registry (commands.go), the palette (07) and keybindings (08) use to drive the
// editor — there is no parallel command-dispatch mechanism.
type ActionMsg struct{ Action string }

// OpenUndoTreeMsg asks the root model to open the undo-tree overlay (#59) for
// the editor that emitted it (always the focused one — the message rises from
// an action dispatched there).
type OpenUndoTreeMsg struct{}

// HistoryJumpMsg asks the focused editor to restore the buffer to the history
// state identified by Seq. Dispatched by the undo-tree overlay.
type HistoryJumpMsg struct{ Seq int }

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
	// A locked dependency file (#565) blocks the edit and stashes it: the host
	// prompts, and a confirm replays exactly this mutation via applyMutate.
	if m.blockDep() {
		m.stashDep(func(mm *Model) { mm.applyMutate(fn) })
		return
	}
	m.applyMutate(fn)
}

// applyMutate is mutate without the dependency-file guard: the recorder-based
// change itself. ConfirmDepEdit replays through here after unlocking.
func (m *Model) applyMutate(fn func(rec *history.Recorder) buffer.Position) {
	rec := history.NewRecorder(m.buf, m.cursor)
	newCur := fn(rec)
	if !rec.Empty() {
		m.hist.Push(rec.Commit(newCur))
		m.dirty = true
		m.emit(EventChange)
	}
	m.cursor = m.buf.ClampCursor(newCur)
	m.desiredCol = m.cursor.Col
	// A mutation that didn't fan out may have shifted text under the
	// secondary carets; snap them back into the buffer (#145).
	m.clampCarets()
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
	// The change deletes before entering insert, so guard before any mutation
	// on a locked dependency file (#565); a confirm replays the whole change.
	if m.blockDep() {
		m.stashDep(func(mm *Model) { mm.beginInsertChange(reg, target) })
		return
	}
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
	// Backstop for insert entries that bypass normalCommand (gi, multi-caret): on
	// a locked dependency file, don't enter insert; stash a replay that opens a
	// fresh (unlocked) recorder once confirmed (#565). The common i/a/o/… entries
	// are already blocked in normalCommand and never reach here while locked.
	if m.blockDep() {
		m.stashDep(func(mm *Model) { mm.startInsertWith(mm.newRecorder(), pre) })
		return
	}
	m.mode = Insert
	m.insert = insertSession{active: true, rec: rec, pre: pre}
	m.emit(EventCursorMove)
}

// commitInsert leaves insert mode, commits the session's recorder as one change,
// and records the dot replay (structural pre + typed text).
func (m *Model) commitInsert() {
	// Leaving the insert session ends the call-typing context, so the
	// signature popup goes with it (#315) — otherwise it would trail the
	// cursor through normal-mode motions, which emit no change events for
	// the server to dismiss it on.
	m.dismissSignature()
	m.snippetEnd() // leaving insert mode ends any snippet tabstop session (#846)
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
	// Secondary carets step back off the one-past-end column the same way;
	// they stay active until an explicit Esc in normal mode collapses them (#145).
	m.moveCarets(false, func(pos buffer.Position, _ int) (buffer.Position, int) {
		if pos.Col > 0 {
			pos.Col--
		}
		return pos, pos.Col
	})
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

// paste inserts the register entry relative to the cursor (p/P), recording a
// dot. With carets active the paste lands at every caret as one undo unit (#145).
func (m *Model) paste(reg rune, after bool, count int, gp bool) {
	e := m.regs.Get(reg)
	if e.Text == "" {
		return
	}
	if m.hasCarets() {
		if e.Linewise {
			m.caretsOnePerLine()
		}
		m.fanMutate(func(rec *history.Recorder, pos, _ buffer.Position) buffer.Position {
			return operator.Paste(m.buf, rec, e, pos, after, count, gp)
		})
		m.dot = &dotCommand{run: func(mm *Model) { mm.paste(reg, after, count, gp) }}
		return
	}
	m.mutate(func(rec *history.Recorder) buffer.Position {
		return operator.Paste(m.buf, rec, e, m.cursor, after, count, gp)
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.paste(reg, after, count, gp) }}
}

// deleteUnderCursor implements x: delete count runes from the cursor — and,
// with carets active, from every caret as one undo unit (#145).
func (m *Model) deleteUnderCursor(reg rune, count int) {
	if m.hasCarets() {
		m.fanMutate(func(rec *history.Recorder, pos, _ buffer.Position) buffer.Position {
			max := m.buf.RuneLen(pos.Line)
			if max == 0 || pos.Col >= max {
				return pos
			}
			end := pos
			if end.Col += count; end.Col > max {
				end.Col = max
			}
			rec.Apply(buffer.Delete(buffer.Range{Start: pos, End: end}))
			return m.buf.ClampCursor(pos)
		})
		m.dot = &dotCommand{run: func(mm *Model) { mm.deleteUnderCursor(reg, count) }}
		return
	}
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

// replaceChar implements r: overwrite the rune under the cursor with ch — at
// every caret when carets are active (#145).
func (m *Model) replaceChar(ch rune, count int) {
	if m.hasCarets() {
		m.fanMutate(func(rec *history.Recorder, pos, _ buffer.Position) buffer.Position {
			if m.buf.RuneLen(pos.Line) < pos.Col+count {
				return pos
			}
			end := buffer.Position{Line: pos.Line, Col: pos.Col + count}
			rec.Apply(buffer.Edit{Range: buffer.Range{Start: pos, End: end}, Text: strings.Repeat(string(ch), count)})
			return buffer.Position{Line: pos.Line, Col: pos.Col + count - 1}
		})
		m.dot = &dotCommand{run: func(mm *Model) { mm.replaceChar(ch, count) }}
		return
	}
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
	// Undo restores one cursor; the fan-out collapses with its change (#145).
	m.collapseCarets()
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
		// An undo mutates the buffer, so the document is dirty again — unless
		// the walk landed exactly on the last-saved state (#251). Without the
		// dirty side, undoing past a save (or an auto-save, #174) would leave
		// changes that never get persisted.
		m.dirty = !m.hist.AtSaved()
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
	m.collapseCarets()
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
		m.dirty = !m.hist.AtSaved()
		m.emit(EventChange)
	}
}

// undoChrono walks count states back in global (seq) order across branches —
// vim's g-. Unlike u it can land on a sibling branch's state.
func (m *Model) undoChrono(count int) {
	m.walkChrono(count, (*history.History).UndoChrono)
}

// redoChrono walks count states forward in global (seq) order — vim's g+.
func (m *Model) redoChrono(count int) {
	m.walkChrono(count, (*history.History).RedoChrono)
}

// walkChrono shares the undo/redo bookkeeping for the chronological walks:
// commit an open insert, collapse carets, step, restore cursor and dirty flag.
func (m *Model) walkChrono(count int, step func(*history.History, *buffer.Buffer) (buffer.Position, bool)) {
	if m.insert.active {
		m.commitInsert()
	}
	m.collapseCarets()
	moved := false
	for i := 0; i < count; i++ {
		cur, ok := step(m.hist, m.buf)
		if !ok {
			break
		}
		moved = true
		m.cursor = m.buf.ClampCursor(cur)
	}
	if moved {
		m.desiredCol = m.cursor.Col
		m.dirty = !m.hist.AtSaved()
		m.emit(EventChange)
	}
}

// jumpHistory restores the buffer to history state seq (undo-tree overlay).
func (m *Model) jumpHistory(seq int) {
	if m.insert.active {
		m.commitInsert()
	}
	m.collapseCarets()
	cur, ok := m.hist.JumpTo(m.buf, seq)
	if !ok {
		return
	}
	m.cursor = m.buf.ClampCursor(cur)
	m.desiredCol = m.cursor.Col
	m.dirty = !m.hist.AtSaved()
	m.emit(EventChange)
}

// HistoryTree exposes the undo tree for the overlay (#59). The undo_tree
// action commits any open insert session before the overlay opens, so the
// tree already includes the in-flight typing.
func (m *Model) HistoryTree() []history.NodeInfo { return m.hist.Tree() }

// save writes the buffer to disk, applying the trim-trailing-whitespace and
// final-newline policies, and clears the dirty flag. No-op without a file.
func (m *Model) save() error { return m.saveAs(m.path) }

// saveAs writes the buffer to path (":w file"). It updates the editor's path on
// success so subsequent saves target the new file. The trim/final-newline
// policies apply to the logical lines; the stored line-ending flavor and
// encoding are re-applied on the way out (#66), so a CRLF or UTF-16 file
// round-trips byte-identically.
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
	out, err := textenc.Encode(data, m.enc, m.eol)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}
	if path != m.path {
		// ":w other" retargets the buffer; its .editorconfig overrides (#63)
		// follow the new path from the next applyConfig pass on.
		m.path = path
		m.resolveEditorconfig()
	}
	m.dirty = false
	m.mixedEOL = false // the write just normalized to m.eol
	m.hist.MarkSaved()
	m.diskHash = ""
	if !m.largeFile { // large-file mode opts out of persistent undo (#149)
		m.diskHash = undostore.Hash(out)
		m.PersistUndo()
	}
	m.emit(EventSave)
	return nil
}

// runAction executes a named action requested via ActionMsg (registry commands).
func (m Model) runAction(action string) (Model, tea.Cmd) {
	switch action {
	case "write":
		if cmd, _ := m.saveGuarded(m.path, false); cmd != nil {
			return m, cmd
		}
	case "quit":
		return m, func() tea.Msg { return CloseMsg{} }
	case "write_quit":
		cmd, ok := m.saveGuarded(m.path, true)
		if cmd != nil {
			return m, cmd // conflict: prompt first, keep the pane open
		}
		if !ok {
			return m, nil // write failed: stay open, the ex line has the error
		}
		return m, func() tea.Msg { return CloseMsg{} }
	case "undo":
		m.undo(1)
	case "redo":
		m.redo(1)
	case "undo_chrono":
		m.undoChrono(1)
	case "redo_chrono":
		m.redoChrono(1)
	case "undo_tree":
		if m.insert.active {
			m.commitInsert()
		}
		return m, func() tea.Msg { return OpenUndoTreeMsg{} }
	case "copy":
		cmd := m.clipboardCopy()
		m.scroll()
		return m, cmd
	case "cut":
		cmd := m.clipboardCut()
		m.scroll()
		return m, cmd
	case "paste":
		m.clipboardPaste()
	case "line_start":
		m.lineStart()
	case "line_end":
		m.lineEnd()
	// View options (#64): per-view display toggles, overriding the [editor]
	// config for this view.
	case "toggle_wrap":
		m.toggleWrap()
	case "toggle_whitespace":
		m.toggleWhitespace()
	case "toggle_indent_guides":
		m.toggleIndentGuides()
	case "find":
		if m.insert.active {
			m.commitInsert()
		}
		m.beginSearch(search.Forward)
	case "replace":
		if m.insert.active {
			m.commitInsert()
		}
		m.beginReplacePanel()
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
	case "next_diagnostic", "prev_diagnostic":
		if m.insert.active {
			m.commitInsert()
		}
		cmd := m.diagnosticJump(action == "next_diagnostic")
		return m, cmd
	case "caret_add_next":
		if m.insert.active {
			m.commitInsert()
		}
		m.caretAddNext()
	case "caret_add_all":
		if m.insert.active {
			m.commitInsert()
		}
		m.caretAddAll()
	case "fold_toggle":
		m.foldToggle()
	case "fold_close":
		m.foldCloseAtCursor()
	case "fold_open":
		m.foldOpenAtCursor()
	case "fold_close_all":
		m.foldCloseAll()
	case "fold_open_all":
		m.foldOpenAll()
	case "eol_lf":
		m.setLineEnding(textenc.LF)
	case "eol_crlf":
		m.setLineEnding(textenc.CRLF)
	case "encoding_utf8":
		m.setEncoding(textenc.UTF8)
	case "encoding_utf8_bom":
		m.setEncoding(textenc.UTF8BOM)
	case "encoding_utf16le":
		m.setEncoding(textenc.UTF16LE)
	case "encoding_utf16be":
		m.setEncoding(textenc.UTF16BE)
	case "encoding_latin1":
		m.setEncoding(textenc.Latin1)
	case "encoding_windows1252":
		m.setEncoding(textenc.Windows1252)
	}
	m.scroll()
	return m, nil
}

// clipboardCopy yanks the visual selection — or the current line when nothing
// is selected — into the system-clipboard register `+` (Cmd+C). The returned
// command is the feedback toast (#252).
func (m *Model) clipboardCopy() tea.Cmd {
	if m.mode.IsVisual() {
		m.visualOperateReg('y', '+')
	} else {
		m.runOperator('y', operator.LineTarget(m.cursor.Line, m.cursor.Line), '+')
	}
	return m.clipboardNotice("copied")
}

// clipboardCut deletes the visual selection — or the current line — into the
// system-clipboard register `+` (Cmd+X). The returned command is the feedback
// toast (#252).
func (m *Model) clipboardCut() tea.Cmd {
	if m.mode.IsVisual() {
		m.visualOperateReg('d', '+')
	} else {
		m.runOperator('d', operator.LineTarget(m.cursor.Line, m.cursor.Line), '+')
	}
	return m.clipboardNotice("cut")
}

// clipboardNotice reports what the copy/cut just put in the clipboard
// ("copied 3 lines", "cut 12 chars"), read from the unnamed register — every
// `+` write mirrors into it, so no system-clipboard read-back is needed.
func (m *Model) clipboardNotice(verb string) tea.Cmd {
	e := m.regs.Get(0)
	if e.Text == "" {
		return nil
	}
	var n int
	unit := "char"
	if e.Linewise {
		unit = "line"
		if n = strings.Count(e.Text, "\n"); n == 0 {
			n = 1
		}
	} else {
		n = len([]rune(e.Text))
	}
	if n != 1 {
		unit += "s"
	}
	return notice(verb + " " + strconv.Itoa(n) + " " + unit)
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

// PasteText inserts external (bracketed-paste) text as a single block and one
// undo unit (#603), so a large paste lands at once — not character by character —
// without disturbing the yank registers or the system clipboard. Visual mode
// replaces the selection; mid-insert it splices into the open insert; normal
// mode pastes after the cursor like `p`. The change emits through mutate, so LSP
// sync and highlighting see one edit.
func (m *Model) PasteText(text string) {
	if text == "" {
		return
	}
	e := register.Entry{Text: text, Linewise: strings.HasSuffix(text, "\n")}
	if m.mode.IsVisual() {
		target := m.visualSelection()
		start := target.Range.Start
		m.mode = Normal
		m.mutate(func(rec *history.Recorder) buffer.Position {
			operator.Delete(m.buf, rec, m.regs, m.pending.Register, target)
			if e.Linewise {
				return operator.Paste(m.buf, rec, e, start, false, 1, false)
			}
			at := m.buf.Clamp(start)
			end := rec.Apply(buffer.Insert(at, e.Text))
			if end.Col > at.Col {
				end.Col--
			}
			return end
		})
		m.pending.Reset()
		return
	}
	if m.insert.active {
		m.insertText(text)
		return
	}
	m.mutate(func(rec *history.Recorder) buffer.Position {
		return operator.Paste(m.buf, rec, e, m.cursor, true, 1, false)
	})
}

// RegisterHistory exposes the yank/delete history, newest first, for the
// paste-from-history picker (#57).
func (m Model) RegisterHistory() []register.Entry { return m.regs.History() }

// PasteHistoryEntry pastes history entry i with Cmd+V semantics (#57),
// JetBrains-style: the chosen entry becomes the current clipboard (and the
// unnamed register), then flows through the normal clipboard-paste path —
// visual selections are replaced, a paste mid-insert joins the open insert's
// undo unit.
func (m *Model) PasteHistoryEntry(i int) {
	h := m.regs.History()
	if i < 0 || i >= len(h) {
		return
	}
	m.regs.Yank('+', h[i])
	m.clipboardPaste()
	m.scroll()
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

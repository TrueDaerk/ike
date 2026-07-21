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
		m.insertNewline()
	// Word/line kills (#246) come before the plain-backspace case, which
	// matches KeyBackspace regardless of modifiers. alt+backspace mirrors the
	// terminal pane's macOS convention (#240), ctrl+w is the vim-native twin;
	// cmd+backspace / ctrl+u kill to the line start the same way.
	case key.Code == tea.KeyBackspace && key.Mod&^tea.ModShift == tea.ModAlt,
		key.Code == 'w' && key.Mod == tea.ModCtrl:
		m.insertKillBack(func(pos buffer.Position) buffer.Position {
			return motion.WordBackward(m.buf, pos, 1).Pos
		})
	case key.Code == tea.KeyBackspace && (key.Mod&^tea.ModShift == tea.ModSuper || key.Mod&^tea.ModShift == tea.ModMeta),
		key.Code == 'u' && key.Mod == tea.ModCtrl:
		m.insertKillBack(func(pos buffer.Position) buffer.Position {
			return buffer.Position{Line: pos.Line, Col: 0}
		})
	case key.Code == tea.KeyBackspace, key.Code == 'h' && key.Mod == tea.ModCtrl:
		m.insertBackspace()
	case key.Code == tea.KeyDelete && key.Mod&^tea.ModShift == 0:
		m.insertForwardDelete()
	// An active snippet session (#846) owns tab/shift+tab: jump between the
	// expansion's tabstops instead of indenting.
	case key.Code == tea.KeyTab && m.snippet != nil && key.Mod&tea.ModShift != 0:
		m.snippetMove(-1)
	case key.Code == tea.KeyTab && m.snippet != nil:
		m.snippetMove(1)
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
		m.moveCarets(true, func(pos buffer.Position, _ int) (buffer.Position, int) {
			return buffer.Position{Line: pos.Line, Col: 0}, 0
		})
	case key.Code == tea.KeyEnd:
		m.cursor = buffer.Position{Line: m.cursor.Line, Col: m.buf.RuneLen(m.cursor.Line)}
		m.desiredCol = m.cursor.Col
		m.moveCarets(true, func(pos buffer.Position, _ int) (buffer.Position, int) {
			c := m.buf.RuneLen(pos.Line)
			return buffer.Position{Line: pos.Line, Col: c}, c
		})
	case key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		// Printable input, including a bare space (Text == " ").
		m.writeRunes(key.Text)
	default:
		// Alt/Ctrl+arrows (word nav), PgUp/PgDn and Ctrl motions also work mid-insert.
		if res, ok := m.resolveMotion(key.String(), 0, 1); ok {
			m.fanMotionSecondaries(key.String(), 0, 1, true)
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
// Secondary carets move in parallel (#145).
func (m *Model) insertMove(dLine, dCol int) {
	// Arrow motion emits a cursor-move event; a showing signature popup
	// follows it via the bridge retrigger (#523) instead of being dismissed
	// outright (#315) — the server answers null once the cursor leaves the
	// call context.
	p := buffer.Position{Line: m.cursor.Line + dLine, Col: m.cursor.Col + dCol}
	m.cursor = m.buf.Clamp(p)
	m.desiredCol = m.cursor.Col
	m.moveCarets(true, func(pos buffer.Position, _ int) (buffer.Position, int) {
		q := m.buf.Clamp(buffer.Position{Line: pos.Line + dLine, Col: pos.Col + dCol})
		return q, q.Col
	})
	m.emit(EventCursorMove)
}

// writeRunes inserts text, overwriting in replace mode and triggering a
// completion event for the typed character.
func (m *Model) writeRunes(text string) {
	if m.mode == Replace {
		m.replaceText(text)
		return
	}
	if !m.autoClosePairs || !m.autoCloseWrite(text) {
		m.insertText(text)
	}
	m.maybeAutoComplete(text)
}

// maybeAutoComplete emits a completion trigger for a single typed character —
// after the insert, so the bridge syncs the new text before the request
// (#527). Which characters actually fire a server request is the bridge's
// call (server trigger characters, identifier runes); the editor only skips
// identifier runes while the popup is showing, because those merely narrow
// the client-side prefix filter and need no re-query.
func (m *Model) maybeAutoComplete(text string) {
	r := []rune(text)
	if len(r) != 1 {
		return // paste or multi-rune input never auto-triggers
	}
	if m.CompletionOpen() && isIdentRune(r[0]) {
		return
	}
	m.emitChar(EventCompletionTrigger, text)
}

// insertText splices text at every caret through the session recorder (#145);
// without secondary carets that is exactly the old single-cursor insert.
func (m *Model) insertText(text string) {
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	m.fanApply(func(pos, _ buffer.Position) buffer.Position {
		return m.insert.rec.Apply(buffer.Insert(pos, text))
	})
	m.insert.typed += text
	m.dirtyFromInsert()
}

// insertNewline splits the line at every caret, applying smart indent per
// caret (a mid-line split indents by the text left of that caret).
func (m *Model) insertNewline() {
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	indentAt := func(pos buffer.Position) string {
		if !m.autoIndent {
			return ""
		}
		left := []rune(m.buf.Line(pos.Line))
		col := min(pos.Col, len(left))
		return m.smartIndent(string(left[:col]))
	}
	// "." replays the primary caret's indent, like the single-cursor insert
	// did; a block split (#518) replays only its pre-cursor half.
	typed := "\n" + indentAt(m.cursor)
	m.fanApply(func(pos, _ buffer.Position) buffer.Position {
		if m.autoIndent {
			if mid, ok := m.splitBlock(pos); ok {
				return mid
			}
		}
		return m.insert.rec.Apply(buffer.Insert(pos, "\n"+indentAt(pos)))
	})
	m.insert.typed += typed
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

// insertBackspace deletes the rune before every caret, joining lines at
// column 0. Carets whose deletions collide merge (#145).
func (m *Model) insertBackspace() {
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	edited := false
	m.fanApply(func(pos, floor buffer.Position) buffer.Position {
		var start buffer.Position
		switch {
		case pos.Col > 0:
			start = buffer.Position{Line: pos.Line, Col: pos.Col - 1}
		case pos.Line > 0:
			start = buffer.Position{Line: pos.Line - 1, Col: m.buf.RuneLen(pos.Line - 1)}
		default:
			return pos
		}
		if floor.Line >= 0 && start.Before(floor) {
			start = floor // never delete into an earlier caret's edit
		}
		if !start.Before(pos) {
			return pos
		}
		end := pos
		// Backspacing the opener of an empty pair removes the closer with
		// it (#517), undoing an auto-close in one keystroke.
		if m.autoClosePairs && start.Line == pos.Line && pos.Col-start.Col == 1 {
			if c, ok := pairCloser(m.runeAt(start)); ok && m.runeAt(pos) == c {
				end.Col++
			}
		}
		m.insert.rec.Apply(buffer.Delete(buffer.Range{Start: start, End: end}))
		edited = true
		return start
	})
	if !edited {
		return
	}
	// Backspace approximately rewinds the recorded text for "." replay.
	if r := []rune(m.insert.typed); len(r) > 0 {
		m.insert.typed = string(r[:len(r)-1])
	}
	m.dirtyFromInsert()
}

// insertForwardDelete deletes the rune after every caret (Del, #732), joining
// with the next line at a line end like vim's <Del>; at the very end of the
// buffer it is a no-op. The caret stays put and the recorded "." text is left
// alone — the deletion happens ahead of anything typed this session.
func (m *Model) insertForwardDelete() {
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	edited := false
	m.fanApply(func(pos, _ buffer.Position) buffer.Position {
		var end buffer.Position
		switch {
		case pos.Col < m.buf.RuneLen(pos.Line):
			end = buffer.Position{Line: pos.Line, Col: pos.Col + 1}
		case pos.Line < m.buf.LineCount()-1:
			end = buffer.Position{Line: pos.Line + 1, Col: 0}
		default:
			return pos
		}
		m.insert.rec.Apply(buffer.Delete(buffer.Range{Start: pos, End: end}))
		edited = true
		return pos
	})
	if !edited {
		return
	}
	m.dirtyFromInsert()
}

// insertKillBack fans a backward kill across every caret: startFor resolves,
// per caret, the position the kill reaches back to (word start, line start).
// The single-caret path keeps the old insertDeleteBack semantics.
func (m *Model) insertKillBack(startFor func(pos buffer.Position) buffer.Position) {
	if !m.hasCarets() {
		m.insertDeleteBack(startFor(m.cursor))
		return
	}
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	edited := false
	m.fanApply(func(pos, floor buffer.Position) buffer.Position {
		start := m.buf.Clamp(startFor(pos))
		if floor.Line >= 0 && start.Before(floor) {
			start = floor
		}
		if !start.Before(pos) {
			return pos
		}
		m.insert.rec.Apply(buffer.Delete(buffer.Range{Start: start, End: pos}))
		edited = true
		return start
	})
	if !edited {
		return
	}
	m.insert.typed = "" // multi-caret kills clear the "." text, like a cross-line kill
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
	// Every caret line dedents once, no matter how many carets sit on it (#145).
	lines := map[int]bool{m.cursor.Line: true}
	for _, c := range m.carets {
		lines[c.pos.Line] = true
	}
	edited := false
	for l := range lines {
		n := dedentCols(m.buf.Line(l), m.tabWidth)
		if n == 0 {
			continue
		}
		if m.insert.rec == nil {
			m.insert.rec = m.newRecorder()
		}
		m.insert.rec.Apply(buffer.Delete(buffer.Range{
			Start: buffer.Position{Line: l, Col: 0},
			End:   buffer.Position{Line: l, Col: n},
		}))
		edited = true
		if m.cursor.Line == l {
			if m.cursor.Col -= n; m.cursor.Col < 0 {
				m.cursor.Col = 0
			}
			m.desiredCol = m.cursor.Col
		}
		for i := range m.carets {
			if m.carets[i].pos.Line != l {
				continue
			}
			if m.carets[i].pos.Col -= n; m.carets[i].pos.Col < 0 {
				m.carets[i].pos.Col = 0
			}
			m.carets[i].desiredCol = m.carets[i].pos.Col
		}
	}
	if !edited {
		return
	}
	m.sortCarets()
	m.dirtyFromInsert()
}

// dirtyFromInsert marks the buffer dirty and emits a change while typing.
func (m *Model) dirtyFromInsert() {
	m.dirty = true
	m.emit(EventChange)
}

package editor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/mode"
	"ike/internal/editor/motion"
	"ike/internal/editor/operator"
	"ike/internal/editor/search"
)

// updateNormal handles a key in normal mode, driving the pending-operator /
// count / register / await-secondary-key state machine.
func (m Model) updateNormal(key tea.KeyPressMsg) (Model, tea.Cmd) {
	s := key.String()
	r, hasRune := firstRune(key)

	// Any normal-mode key clears a lingering ex-command message (vim leaves the
	// last ":"-line message up until the next key).
	m.cmdMsg = ""

	// Esc dismisses search-match highlights (vim's :noh, #255); n/N/* re-arm.
	if key.Code == tea.KeyEscape {
		m.hlActive = false
	}

	// Secondary-key states resolve before anything else.
	switch m.wait {
	case awaitG:
		m.wait = awaitNone
		return m.resolveAfterG(s, r, hasRune)
	case awaitFind:
		m.wait = awaitNone
		if hasRune {
			m.lastFind = motion.Find{Kind: m.findCmd, Char: r}
			m.applyFind(m.lastFind)
		}
		m.pending.Reset()
		return m, nil
	case awaitReplace:
		m.wait = awaitNone
		if hasRune {
			m.replaceChar(r, m.pending.EffectiveCount())
		}
		m.pending.Reset()
		return m, nil
	case awaitObject:
		m.wait = awaitNone
		if hasRune {
			m.applyTextObject(r)
		}
		m.pending.Reset()
		return m, nil
	}

	// Register selection: `"` then a name.
	if m.pending.AwaitingRegister() {
		if hasRune {
			m.pending.SetRegister(r)
		}
		return m, nil
	}
	if s == `"` {
		m.pending.BeginRegister()
		return m, nil
	}

	// Counts: 1-9 always; 0 only continues an existing count (else it is a motion).
	if hasRune && r >= '1' && r <= '9' {
		m.pending.PushDigit(int(r - '0'))
		return m, nil
	}
	if s == "0" && m.pending.Count > 0 {
		m.pending.PushDigit(0)
		return m, nil
	}

	count := m.pending.EffectiveCount()

	// Operators: a doubled operator (dd/cc/yy) is linewise on count lines.
	if op, ok := operatorKey(s); ok {
		if m.pending.Operator == op {
			m.applyLinewiseOperator(op, count)
			m.pending.Reset()
			return m, nil
		}
		if m.pending.Operator == 0 {
			m.pending.SetOperator(op)
			return m, nil
		}
	}

	// Text-object intro (i/a) while an operator is pending.
	if m.pending.HasOperator() && (s == "i" || s == "a") {
		m.around = s == "a"
		m.wait = awaitObject
		return m, nil
	}

	// Shift+arrows start a charwise selection: enter visual mode anchored at the
	// cursor and apply the plain arrow motion (updateVisual extends it further).
	if plain, ok := shiftSelectKey(s); ok && !m.pending.HasOperator() {
		m.enterVisual(Visual)
		if res, ok := m.resolveMotion(plain, 0, count); ok {
			m.applyMotionOrOperator(res, count)
		}
		return m, nil
	}

	// Motions (also serve as operator targets).
	if res, ok := m.resolveMotion(s, r, count); ok {
		m.applyMotionOrOperator(res, count)
		return m, nil
	}

	// Find motions need a target char next.
	if fk, ok := findKey(s); ok {
		m.findCmd = fk
		m.wait = awaitFind
		return m, nil
	}
	if s == ";" && m.lastFind.Valid() {
		m.applyFind(m.lastFind.Repeat())
		m.pending.Reset()
		return m, nil
	}
	if s == "," && m.lastFind.Valid() {
		m.applyFind(m.lastFind.Reverse())
		m.pending.Reset()
		return m, nil
	}

	// Non-operator commands.
	if m.pending.HasOperator() {
		// An operator awaiting a motion got something it can't use: cancel.
		m.pending.Reset()
		return m, nil
	}
	return m.normalCommand(s, r, count)
}

// applyMotionOrOperator either moves the cursor or, when an operator is pending,
// composes the motion into a target and applies the operator.
func (m *Model) applyMotionOrOperator(res motion.Result, count int) {
	if m.pending.HasOperator() {
		target := operator.Compose(m.buf, m.cursor, res.Pos, res.Kind)
		m.runOperator(m.pending.Operator, target, m.pending.Register)
		m.pending.Reset()
		return
	}
	if res.Jump {
		// The departure point of a large motion belongs in the navigation
		// history (Roadmap 0220); emitted before the cursor moves.
		m.emit(EventJump)
	}
	if res.Kind == motion.Linewise {
		// Vertical motion keeps the remembered column.
		m.cursor = m.buf.ClampCursor(buffer.Position{Line: res.Pos.Line, Col: m.desiredCol})
		m.emit(EventCursorMove)
	} else {
		m.moveTo(res.Pos)
	}
	m.pending.Reset()
}

// resolveMotion maps a key to a motion Result. ok is false for non-motion keys.
func (m *Model) resolveMotion(s string, r rune, count int) (motion.Result, bool) {
	switch s {
	case "h", "left", "backspace":
		return motion.Left(m.buf, m.cursor, count), true
	case "l", "right", " ":
		return motion.Right(m.buf, m.cursor, count), true
	case "j", "down":
		return motion.Down(m.buf, m.cursor, count), true
	case "k", "up":
		return motion.Up(m.buf, m.cursor, count), true
	case "0", "home":
		return motion.LineStart(m.buf, m.cursor, count), true
	case "^":
		return motion.FirstNonBlank(m.buf, m.cursor, count), true
	case "$", "end":
		return motion.LineEnd(m.buf, m.cursor, count), true
	case "w":
		return motion.WordForward(m.buf, m.cursor, count), true
	case "W":
		return motion.WordForwardBig(m.buf, m.cursor, count), true
	case "b":
		return motion.WordBackward(m.buf, m.cursor, count), true
	case "B":
		return motion.WordBackwardBig(m.buf, m.cursor, count), true
	case "e":
		return motion.WordEnd(m.buf, m.cursor, count), true
	case "E":
		return motion.WordEndBig(m.buf, m.cursor, count), true
	case "{":
		return motion.ParagraphBackward(m.buf, m.cursor, count), true
	case "}":
		return motion.ParagraphForward(m.buf, m.cursor, count), true
	case "G":
		res := motion.Last(m.buf, m.cursor, countOrZero(m.pending))
		res.Jump = true // G / {count}G is a jump (Roadmap 0220)
		return res, true
	case "%":
		if res, ok := motion.MatchPair(m.buf, m.cursor, count); ok {
			return res, true
		}
		return motion.Result{}, false

	// Word navigation with Option/Alt+Left/Right (#303): word-wise within the
	// current line, '.' counts as a stop point. Paragraph jumps with
	// Alt+Up/Down. Ctrl variants are the everywhere-deliverable fallback.
	// Shift+arrows are selection keys, handled before motion resolution in
	// normal and visual mode; the shifted chords resolve here only for
	// insert-mode movement.
	case "alt+right", "ctrl+right", "alt+shift+right", "ctrl+shift+right":
		return motion.WordForwardInLine(m.buf, m.cursor, count), true
	case "alt+left", "ctrl+left", "alt+shift+left", "ctrl+shift+left":
		return motion.WordBackwardInLine(m.buf, m.cursor, count), true
	case "alt+down", "ctrl+down":
		return motion.ParagraphForward(m.buf, m.cursor, count), true
	case "alt+up", "ctrl+up":
		return motion.ParagraphBackward(m.buf, m.cursor, count), true

	// Page and half-page scrolling.
	case "pgdown", "ctrl+f":
		return m.pageMotion(count, false), true
	case "pgup", "ctrl+b":
		return m.pageMotion(-count, false), true
	case "ctrl+d":
		return m.pageMotion(count, true), true
	case "ctrl+u":
		return m.pageMotion(-count, true), true

	// Screen-relative jumps.
	case "H":
		return motion.Result{Pos: buffer.Position{Line: m.view.Top}, Kind: motion.Linewise}, true
	case "L":
		return motion.Result{Pos: buffer.Position{Line: m.view.Bottom(m.buf.LineCount()) - 1}, Kind: motion.Linewise}, true
	case "M":
		mid := (m.view.Top + m.view.Bottom(m.buf.LineCount()) - 1) / 2
		return motion.Result{Pos: buffer.Position{Line: mid}, Kind: motion.Linewise}, true
	}
	return motion.Result{}, false
}

// shiftSelectKey maps a Shift+arrow chord to the plain motion key a selection
// extends with; ok is false for every other key.
func shiftSelectKey(s string) (string, bool) {
	switch s {
	case "shift+left":
		return "left", true
	case "shift+right":
		return "right", true
	case "shift+up":
		return "up", true
	case "shift+down":
		return "down", true
	case "shift+home":
		return "home", true
	case "shift+end":
		return "end", true
	// Shift+opt (and the delivered ctrl fallback) extend the selection
	// word-wise within the line (#303), consistent with shift+arrows (#47).
	case "alt+shift+left":
		return "alt+left", true
	case "alt+shift+right":
		return "alt+right", true
	case "ctrl+shift+left":
		return "ctrl+left", true
	case "ctrl+shift+right":
		return "ctrl+right", true
	}
	return "", false
}

// pageMotion computes a vertical jump of a full or half page in the given
// direction (sign of pages), used by Ctrl-f/b/d/u and PgUp/PgDn.
func (m *Model) pageMotion(pages int, half bool) motion.Result {
	h := m.view.Height()
	if h < 1 {
		h = 1
	}
	step := h
	if half {
		step = h / 2
		if step < 1 {
			step = 1
		}
	}
	line := m.cursor.Line + pages*step
	if line < 0 {
		line = 0
	}
	if line > m.buf.LineCount()-1 {
		line = m.buf.LineCount() - 1
	}
	return motion.Result{Pos: buffer.Position{Line: line, Col: m.desiredCol}, Kind: motion.Linewise}
}

// normalCommand handles non-motion normal-mode keys (edits, mode changes, etc.).
func (m Model) normalCommand(s string, r rune, count int) (Model, tea.Cmd) {
	switch s {
	case "i":
		m.startInsertWith(m.newRecorder(), nil)
	case "I":
		rec := m.newRecorder()
		m.cursor = motion.FirstNonBlank(m.buf, m.cursor, 1).Pos
		m.startInsertWith(rec, func(mm *Model, _ *history.Recorder) buffer.Position {
			mm.cursor = motion.FirstNonBlank(mm.buf, mm.cursor, 1).Pos
			return mm.cursor
		})
	case "a":
		rec := m.newRecorder()
		m.cursorRightForAppend()
		m.startInsertWith(rec, func(mm *Model, _ *history.Recorder) buffer.Position {
			mm.cursorRightForAppend()
			return mm.cursor
		})
	case "A":
		rec := m.newRecorder()
		m.cursor = buffer.Position{Line: m.cursor.Line, Col: m.buf.RuneLen(m.cursor.Line)}
		m.startInsertWith(rec, func(mm *Model, _ *history.Recorder) buffer.Position {
			mm.cursor = buffer.Position{Line: mm.cursor.Line, Col: mm.buf.RuneLen(mm.cursor.Line)}
			return mm.cursor
		})
	case "o":
		m.openLine(true)
	case "O":
		m.openLine(false)
	case "x":
		m.deleteUnderCursor(m.pending.Register, count)
	case "D":
		m.runOperator('d', operator.Compose(m.buf, m.cursor, motion.LineEnd(m.buf, m.cursor, 1).Pos, motion.Inclusive), m.pending.Register)
	case "C":
		m.runOperator('c', operator.Compose(m.buf, m.cursor, motion.LineEnd(m.buf, m.cursor, 1).Pos, motion.Inclusive), m.pending.Register)
	case "Y":
		m.applyLinewiseOperator('y', count)
	case "s":
		m.deleteUnderCursor(m.pending.Register, count)
		m.startInsertWith(m.newRecorder(), nil)
	case "r":
		m.wait = awaitReplace
		return m, nil
	case "R":
		m.mode = Replace
		m.insert = insertSession{active: true, rec: m.newRecorder()}
	case "p":
		m.paste(m.pending.Register, true, count, false)
	case "P":
		m.paste(m.pending.Register, false, count, false)
	case "J":
		m.joinLines(count)
	case "~":
		m.toggleCase(count)
	case "*":
		m.searchWord(true)
	case "#":
		m.searchWord(false)
	case "u":
		m.undo(count)
	case "ctrl+r":
		m.redo(count)
	case ".":
		m.repeatDot(count)
	case "g":
		m.wait = awaitG
		return m, nil
	case "v":
		m.enterVisual(Visual)
	case "V":
		m.enterVisual(mode.VisualLine)
	case "ctrl+v":
		m.enterVisual(mode.VisualBlock)
	case "n":
		m.searchNextRepeat(false, count)
	case "N":
		m.searchNextRepeat(true, count)
	case "/":
		m.beginSearch(search.Forward)
		return m, nil
	case "?":
		m.beginSearch(search.Backward)
		return m, nil
	case ":":
		m.mode = Command
		m.cmdline = ""
	}
	m.pending.Reset()
	return m, nil
}

// resolveAfterG handles the second key of a "g" sequence.
func (m Model) resolveAfterG(s string, r rune, hasRune bool) (Model, tea.Cmd) {
	switch s {
	case "g":
		res := motion.First(m.buf, m.cursor, countOrZero(m.pending))
		res.Jump = true // gg is a jump (Roadmap 0220)
		m.applyMotionOrOperator(res, m.pending.EffectiveCount())
	case "p":
		m.paste(m.pending.Register, true, m.pending.EffectiveCount(), true)
		m.pending.Reset()
	}
	return m, nil
}

// operatorKey reports whether s is an operator key and which one.
func operatorKey(s string) (rune, bool) {
	switch s {
	case "d":
		return 'd', true
	case "c":
		return 'c', true
	case "y":
		return 'y', true
	case ">":
		return '>', true
	case "<":
		return '<', true
	}
	return 0, false
}

// findKey maps f/t/F/T to a FindKind.
func findKey(s string) (motion.FindKind, bool) {
	switch s {
	case "f":
		return motion.FindForward, true
	case "t":
		return motion.TillForward, true
	case "F":
		return motion.FindBackward, true
	case "T":
		return motion.TillBackward, true
	}
	return 0, false
}

// firstRune returns the single rune of a printable key, if it is one. A bare
// space arrives as Text == " ".
func firstRune(key tea.KeyPressMsg) (rune, bool) {
	if r := []rune(key.Text); len(r) == 1 {
		return r[0], true
	}
	return 0, false
}

// countOrZero returns the explicit count or 0 (so gg/G default to first/last).
func countOrZero(p mode.Pending) int { return p.Count }

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
	// With multiple carets active it collapses them to the primary first (#145).
	if key.Code == tea.KeyEscape {
		m.hlActive = false
		m.collapseCarets()
	}

	// Secondary-key states resolve before anything else.
	switch m.wait {
	case awaitG:
		m.wait = awaitNone
		return m.resolveAfterG(s, r, hasRune)
	case awaitBracketF, awaitBracketB:
		// ]c / [c: git hunk navigation (#1170); any other continuation is
		// dropped, like the other pending states.
		forward := m.wait == awaitBracketF
		m.wait = awaitNone
		if s == "c" {
			return m, m.hunkJump(forward)
		}
		return m, nil
	case awaitZ:
		m.wait = awaitNone
		return m.resolveAfterZ(s)
	case awaitZBig:
		// ZZ / ZQ (#1193): save-and-close / close-without-saving, mirroring
		// ":x" and ":q!"; any other continuation cancels.
		m.wait = awaitNone
		switch s {
		case "Z":
			c, ok := m.saveGuarded(m.path, true)
			if c != nil {
				return m, c // conflict: prompt first, keep the pane open
			}
			if !ok {
				return m, nil // write failed: stay open
			}
			return m, func() tea.Msg { return CloseMsg{} }
		case "Q":
			return m, func() tea.Msg { return CloseMsg{Force: true} }
		}
		return m, nil
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
	case awaitRecordReg:
		// q's register name (#58): only a-z starts a recording, anything else
		// cancels — the two pending keys (q + name) were never recorded, so
		// nothing needs dropping.
		m.wait = awaitNone
		if hasRune && macroRegister(r) {
			m.startRecording(r)
		}
		m.pending.Reset()
		return m, nil
	case awaitMark:
		// m's mark name (#1151): a-z is a local mark, A-Z a global one;
		// anything else cancels.
		m.wait = awaitNone
		if hasRune {
			m.setMark(r)
		}
		m.pending.Reset()
		return m, nil
	case awaitMarkLine, awaitMarkExact:
		// ' / backtick jump target (#1151): ' lands on the line's first
		// non-blank, backtick on the exact position. Global marks resolve
		// app-side (cross-file), so the jump may return a command.
		exact := m.wait == awaitMarkExact
		m.wait = awaitNone
		m.pending.Reset()
		if hasRune {
			return m, m.jumpMark(r, exact)
		}
		return m, nil
	case awaitSurrMotion:
		// ys's span (#1475): a motion, i/a introducing a text object, or a
		// second s for the whole line; anything else cancels.
		m.wait = awaitNone
		if s == "i" || s == "a" {
			m.around = s == "a"
			m.wait = awaitSurrObject
			return m, nil
		}
		if s == "s" {
			m.surrResolve = func(mm *Model, pos buffer.Position) (operator.Target, bool) {
				return operator.LineTarget(pos.Line, pos.Line), true
			}
			m.wait = awaitSurrAdd
			return m, nil
		}
		count := m.pending.EffectiveCount()
		if _, ok := m.resolveMotion(s, r, count); ok {
			sk, rk := s, r
			m.surrResolve = func(mm *Model, pos buffer.Position) (operator.Target, bool) {
				return mm.caretTarget(sk, rk, count, pos)
			}
			m.wait = awaitSurrAdd
			return m, nil
		}
		m.pending.Reset()
		return m, nil
	case awaitSurrObject:
		m.wait = awaitNone
		if hasRune {
			rk, around := r, m.around
			m.surrResolve = func(mm *Model, pos buffer.Position) (operator.Target, bool) {
				savedCur, savedAround := mm.cursor, mm.around
				mm.cursor, mm.around = pos, around
				res := mm.resolveTextObject(rk)
				mm.cursor, mm.around = savedCur, savedAround
				if !res.OK {
					return operator.Target{}, false
				}
				return objectTarget(res), true
			}
			m.wait = awaitSurrAdd
			return m, nil
		}
		m.pending.Reset()
		return m, nil
	case awaitSurrAdd:
		m.wait = awaitNone
		if hasRune && m.surrResolve != nil {
			m.surroundAdd(m.surrResolve, r)
		}
		m.surrResolve = nil
		m.pending.Reset()
		return m, nil
	case awaitSurrDelete:
		m.wait = awaitNone
		if hasRune {
			m.surroundDelete(r)
		}
		m.pending.Reset()
		return m, nil
	case awaitSurrChange:
		m.wait = awaitNone
		if hasRune {
			m.surrOld = r
			m.wait = awaitSurrChangeNew
		} else {
			m.pending.Reset()
		}
		return m, nil
	case awaitSurrChangeNew:
		m.wait = awaitNone
		if hasRune {
			m.surroundChange(m.surrOld, r)
		}
		m.pending.Reset()
		return m, nil
	case awaitLabel:
		// Label jump (#787): the key routes through the leap session — target
		// characters first, then a label key; esc (no rune) cancels.
		m.wait = awaitNone
		return m.labelJumpKey(r, hasRune)
	case awaitPlayReg:
		// @'s register name (#58): @@ repeats the last replay; the count typed
		// before @ (5@a) is still pending here.
		m.wait = awaitNone
		count := m.pending.EffectiveCount()
		m.pending.Reset()
		if hasRune {
			if r == '@' {
				r = m.lastMacro
			}
			if macroRegister(r) {
				return m.playMacro(r, count)
			}
		}
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

	// Surround intro (#1475): "s" after a pending y/d/c starts a vim-surround
	// operation (ys{motion}{pair} / ds{pair} / cs{old}{new}) instead of
	// cancelling the operator.
	if m.pending.HasOperator() && s == "s" {
		switch m.pending.Operator {
		case 'y':
			m.wait = awaitSurrMotion
			return m, nil
		case 'd':
			m.wait = awaitSurrDelete
			return m, nil
		case 'c':
			m.wait = awaitSurrChange
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
		m.shiftSelect = true
		if res, ok := m.resolveMotion(plain, 0, count); ok {
			m.applyMotionOrOperator(res, count)
		}
		return m, nil
	}

	// Motions (also serve as operator targets). With carets active, an
	// operator fans out per caret and a bare motion moves every caret (#145).
	if res, ok := m.resolveMotion(s, r, count); ok {
		if m.hasCarets() && m.pending.HasOperator() {
			m.fanOperatorMotion(s, r, count)
			m.pending.Reset()
			return m, nil
		}
		if !m.pending.HasOperator() {
			m.fanMotionSecondaries(s, r, count, false)
		}
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

	// The case/reflow operators double with their own bare key (guu, gUU,
	// g~~, gqq) — the second key is not an operator key itself (#1193).
	if m.pending.HasOperator() && caseOperator(m.pending.Operator) && len(s) == 1 && rune(s[0]) == m.pending.Operator {
		m.applyLinewiseOperator(m.pending.Operator, count)
		m.pending.Reset()
		return m, nil
	}

	// "g" opens the g-sequence layer — also while an operator is pending, so
	// gu/gU/g~/gq compose and d ge / c gg reach their motions (#1193).
	if s == "g" {
		m.wait = awaitG
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
		// Vertical motion keeps the remembered column — in a table-rendered
		// csv/tsv the remembered *table* column instead (#1744).
		m.cursor = m.buf.ClampCursor(buffer.Position{Line: res.Pos.Line, Col: m.svVerticalCol(res.Pos.Line)})
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
		if m.softWrap {
			// Soft wrap (#64): j moves one visual row (vim's gj); the motion
			// is fold-aware, so it also covers collapsed folds.
			return m.wrapVertical(count, 1), true
		}
		if m.hasFolds() {
			// A collapsed fold is one row for vertical motion (#144).
			return m.foldVertical(count, 1), true
		}
		return motion.Down(m.buf, m.cursor, count), true
	case "k", "up":
		if m.softWrap {
			return m.wrapVertical(count, -1), true
		}
		if m.hasFolds() {
			return m.foldVertical(count, -1), true
		}
		return motion.Up(m.buf, m.cursor, count), true
	case "0":
		return motion.LineStart(m.buf, m.cursor, count), true
	case "home":
		return motion.SmartHome(m.buf, m.cursor, count), true
	case "^":
		return motion.FirstNonBlank(m.buf, m.cursor, count), true
	case "$":
		return motion.LineEnd(m.buf, m.cursor, count), true
	case "end":
		return motion.SmartEnd(m.buf, m.cursor, count), true
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
	// alt+b/alt+f are the readline sequences terminals synthesize for
	// Option+Arrows (ESC b / ESC f, #1583). Shift+arrows are selection keys,
	// handled before motion resolution in normal and visual mode; the shifted
	// chords resolve here only for insert-mode movement.
	case "alt+right", "ctrl+right", "alt+shift+right", "ctrl+shift+right", "alt+f":
		return motion.WordForwardInLine(m.buf, m.cursor, count), true
	case "alt+left", "ctrl+left", "alt+shift+left", "ctrl+shift+left", "alt+b":
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

// stopSelectKey reports whether s is an unshifted navigation key that ends a
// Shift+arrow selection (vim's keymodel=stopsel, #326). Deliberately limited
// to the keys that can also start/extend a selection with Shift held — vim
// motions (h/l/w/…) keep extending, as in vim.
func stopSelectKey(s string) bool {
	switch s {
	case "left", "right", "up", "down", "home", "end",
		"alt+left", "alt+right", "ctrl+left", "ctrl+right",
		"alt+up", "alt+down", "ctrl+up", "ctrl+down",
		"pgup", "pgdown":
		return true
	}
	return false
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

// insertEntryCmd reports whether a normal-mode command enters insert/replace
// mode (possibly after a structural edit). These are guarded ahead of the
// switch on a locked dependency file (#565) — the destructive one-shots (x, d,
// p, …) are already guarded deeper, at mutate/beginInsertChange.
func insertEntryCmd(s string) bool {
	switch s {
	case "i", "I", "a", "A", "o", "O", "s", "R":
		return true
	}
	return false
}

// normalCommand handles non-motion normal-mode keys (edits, mode changes, etc.).
func (m Model) normalCommand(s string, r rune, count int) (Model, tea.Cmd) {
	// A read-only buffer refuses every insert entry with a message (#1762).
	if m.readOnly && insertEntryCmd(s) {
		m.cmdMsg = roMessage
		return m, nil
	}
	// Entering insert/replace on a locked dependency file blocks and stashes the
	// whole command, so a confirm replays it (including any structural edit like
	// o/O's new line or s's delete). See depedit.go (#565).
	if m.blockDep() && insertEntryCmd(s) {
		m.stashDep(func(mm *Model) {
			nm, _ := mm.normalCommand(s, r, count)
			*mm = nm
		})
		return m, nil
	}
	switch s {
	case "i":
		m.startInsertWith(m.newRecorder(), nil)
	case "I":
		rec := m.newRecorder()
		m.cursor = motion.FirstNonBlank(m.buf, m.cursor, 1).Pos
		m.moveCarets(true, func(pos buffer.Position, _ int) (buffer.Position, int) {
			p := motion.FirstNonBlank(m.buf, pos, 1).Pos
			return p, p.Col
		})
		m.startInsertWith(rec, func(mm *Model, _ *history.Recorder) buffer.Position {
			mm.cursor = motion.FirstNonBlank(mm.buf, mm.cursor, 1).Pos
			return mm.cursor
		})
	case "a":
		rec := m.newRecorder()
		m.cursorRightForAppend()
		m.moveCarets(true, func(pos buffer.Position, _ int) (buffer.Position, int) {
			if m.buf.RuneLen(pos.Line) > 0 {
				pos.Col++
			}
			return pos, pos.Col
		})
		m.startInsertWith(rec, func(mm *Model, _ *history.Recorder) buffer.Position {
			mm.cursorRightForAppend()
			return mm.cursor
		})
	case "A":
		rec := m.newRecorder()
		m.cursor = buffer.Position{Line: m.cursor.Line, Col: m.buf.RuneLen(m.cursor.Line)}
		m.moveCarets(true, func(pos buffer.Position, _ int) (buffer.Position, int) {
			c := m.buf.RuneLen(pos.Line)
			return buffer.Position{Line: pos.Line, Col: c}, c
		})
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
		m.collapseCarets() // replace mode is single-caret (#145)
		m.mode = Replace
		m.insert = insertSession{active: true, rec: m.newRecorder()}
	case "p":
		m.paste(m.pending.Register, true, count, false)
	case "P":
		m.paste(m.pending.Register, false, count, false)
	case "J":
		m.joinLines(count, true)
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
	case "ctrl+a":
		// Increment the number under (or after) the cursor (#1658); the count
		// is the step, so 5<C-a> adds five.
		m.adjustNumber(int64(count))
	case "ctrl+x":
		m.adjustNumber(-int64(count))
	case ".":
		m.collapseCarets() // "." repeats the recorded change at the primary caret
		m.repeatDot(count)
	case "z":
		m.wait = awaitZ
		return m, nil
	case "Z":
		m.wait = awaitZBig
		return m, nil
	case "v":
		m.collapseCarets() // visual selections are single-caret (#145)
		m.enterVisual(Visual)
	case "V":
		m.collapseCarets()
		m.enterVisual(mode.VisualLine)
	case "ctrl+v":
		m.collapseCarets()
		m.enterVisual(mode.VisualBlock)
	case "n":
		m.searchNextRepeat(false, count)
	case "N":
		m.searchNextRepeat(true, count)
	case "/":
		m.collapseCarets() // the command line and search are single-caret (#145)
		m.beginSearch(search.Forward)
		return m, nil
	case "?":
		m.collapseCarets()
		m.beginSearch(search.Backward)
		return m, nil
	case ":":
		m.collapseCarets()
		m.mode = Command
		m.cmdline = ""
		m.cmdCur = 0
		m.cmdHistIdx = -1 // a fresh line starts outside history recall (#1171)
	case "q":
		// Macro recording (#58): q stops an active recording, otherwise the
		// next key names the register to record into. Like vim, a q replayed
		// from a macro neither stops nor starts a recording.
		if m.replayDepth > 0 {
			break
		}
		if m.recordReg != 0 {
			m.stopRecording()
			break
		}
		m.wait = awaitRecordReg
		return m, nil
	case "m":
		// Vim marks (#1151): the next key names the mark to set.
		m.wait = awaitMark
		return m, nil
	case "]":
		m.wait = awaitBracketF
	case "[":
		m.wait = awaitBracketB
	case "'":
		m.wait = awaitMarkLine
		return m, nil
	case "`":
		m.wait = awaitMarkExact
		return m, nil
	case "@":
		// Macro replay (#58): the next key names the register (or @ for the
		// last one). The pending count survives until the name resolves.
		m.wait = awaitPlayReg
		return m, nil
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
	case "-":
		// g-: chronological undo across branches (#59).
		m.undoChrono(m.pending.EffectiveCount())
		m.pending.Reset()
	case "+":
		// g+: chronological redo across branches (#59).
		m.redoChrono(m.pending.EffectiveCount())
		m.pending.Reset()
	case ";":
		// g;: cursor to older edit positions on the change list (#1174) —
		// unlike g-/g+ the buffer is untouched.
		m.changeListJump(true, m.pending.EffectiveCount())
		m.pending.Reset()
	case ",":
		// g,: back toward newer edit positions (#1174).
		m.changeListJump(false, m.pending.EffectiveCount())
		m.pending.Reset()
	case "u", "U", "~", "q":
		// Case operators gu/gU/g~ and the reflow operator gq (#1193). A
		// repeated sequence (gugu) is the linewise form, like guu.
		op := rune(s[0])
		if m.pending.Operator == op {
			m.applyLinewiseOperator(op, m.pending.EffectiveCount())
			m.pending.Reset()
		} else if !m.pending.HasOperator() {
			m.pending.SetOperator(op)
		} else {
			m.pending.Reset()
		}
	case "e":
		// ge: end of the previous word (#1193).
		m.applyMotionOrOperator(motion.WordEndBackward(m.buf, m.cursor, m.pending.EffectiveCount()), m.pending.EffectiveCount())
	case "E":
		m.applyMotionOrOperator(motion.WordEndBackwardBig(m.buf, m.cursor, m.pending.EffectiveCount()), m.pending.EffectiveCount())
	case "J":
		// gJ: join without inserting a space (#1193).
		m.joinLines(m.pending.EffectiveCount(), false)
		m.pending.Reset()
	case "v":
		// gv: reselect the last visual selection (#1193).
		m.reselectVisual()
		m.pending.Reset()
	case "i":
		// gi: insert at the position of the last insert (#1193).
		m.gotoLastInsert()
		m.pending.Reset()
	case "!":
		// g!: toggle the value under the cursor (#1658) — true/false, on/off,
		// ==/!= and friends. Rebindable through editor.toggleValue.
		m.toggleValue()
		m.pending.Reset()
	case "f":
		// gf: open the file named under the cursor (#1193).
		cmd := m.openFileUnderCursor()
		m.pending.Reset()
		return m, cmd
	case "?":
		// g?: explain the concealed or masked value at the caret (#1998) —
		// which rule fired, what it decided, and the keys that overrule it.
		cmd := m.explainConceal()
		m.pending.Reset()
		return m, cmd
	case "s":
		// gs: label jump (#787) — a bare motion to a visible, labeled match.
		// It cannot serve as an operator target, so a pending operator cancels.
		if m.pending.HasOperator() {
			m.pending.Reset()
			return m, nil
		}
		m.pending.Reset()
		m.startLabelJump()
	case "0", "$", "j", "k":
		// Display-line motions (#1193): visual rows under soft wrap, their
		// buffer-line counterparts otherwise.
		if res, ok := m.displayMotion(s, m.pending.EffectiveCount()); ok {
			m.applyMotionOrOperator(res, m.pending.EffectiveCount())
		}
	default:
		// An unrecognised g-sequence cancels whatever was pending — without
		// this a "g"-entered operator would stay armed for the next motion.
		m.pending.Reset()
	}
	return m, nil
}

// resolveAfterZ handles the second key of a "z" sequence — the vim fold
// commands (#144): toggle / close / open the fold at the cursor, close or
// open all folds.
func (m Model) resolveAfterZ(s string) (Model, tea.Cmd) {
	switch s {
	case "a":
		m.foldToggle()
	case "c":
		m.foldCloseAtCursor()
	case "o":
		m.foldOpenAtCursor()
	case "M":
		m.foldCloseAll()
	case "R":
		m.foldOpenAll()
	case "y":
		// zy copies the fold under the cursor whole (#1787).
		cmd := m.foldCopy()
		m.pending.Reset()
		return m, cmd
	case "z":
		m.scrollCursorLine(0)
	case "t":
		m.scrollCursorLine(-1)
	case "b":
		m.scrollCursorLine(1)
	}
	m.pending.Reset()
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
	case "=":
		return '=', true
	}
	return 0, false
}

// caseOperator reports whether op doubles with its own bare key rather than an
// operator key: the g-prefixed case/reflow operators (#1193).
func caseOperator(op rune) bool {
	switch op {
	case 'u', 'U', '~', 'q':
		return true
	}
	return false
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

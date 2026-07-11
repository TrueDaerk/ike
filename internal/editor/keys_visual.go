package editor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/mode"
	"ike/internal/editor/motion"
	"ike/internal/editor/operator"
)

// enterVisual switches to a visual mode, anchoring the selection at the cursor.
// The cursor-move event tells selection listeners (the LSP bridge) a selection
// now exists, before any motion extends it.
func (m *Model) enterVisual(md mode.Mode) {
	m.mode = md
	m.anchor = m.cursor
	m.shiftSelect = false
	m.emit(EventCursorMove)
}

// updateVisual handles keys while a selection is active. Motions extend the
// selection; d/c/y act on it; v/V/ctrl+v toggle or switch the visual variant.
func (m Model) updateVisual(key tea.KeyPressMsg) (Model, tea.Cmd) {
	s := key.String()
	r, hasRune := firstRune(key)

	if key.Code == tea.KeyEscape {
		m.mode = Normal
		m.wait = awaitNone
		m.shiftSelect = false
		m.pending.Reset()
		// Tell listeners the selection is gone (the LSP bridge tracks it for
		// range-scoped requests).
		m.emit(EventCursorMove)
		return m, nil
	}

	// A pending text-object selector (after i/a) sets the selection to the object.
	if m.wait == awaitObject {
		m.wait = awaitNone
		if hasRune {
			m.visualTextObject(r)
		}
		return m, nil
	}

	// A selection started with Shift+arrows is dropped by an unshifted
	// navigation key (vim's keymodel=stopsel, #326): back to normal mode,
	// where the key moves the cursor like any plain motion.
	if m.shiftSelect {
		if _, shifted := shiftSelectKey(s); !shifted && stopSelectKey(s) {
			m.mode = Normal
			m.shiftSelect = false
			m.pending.Reset()
			m.emit(EventCursorMove)
			return m.updateNormal(key)
		}
	}

	// Shift+arrows extend the selection like their plain counterparts.
	if plain, ok := shiftSelectKey(s); ok {
		s = plain
	}

	// Counts: 1-9 always; 0 only continues an existing count (else it is a
	// motion) — the same rule as normal mode.
	if hasRune && r >= '1' && r <= '9' {
		m.pending.PushDigit(int(r - '0'))
		return m, nil
	}
	if s == "0" && m.pending.Count > 0 {
		m.pending.PushDigit(0)
		return m, nil
	}

	// Motions move the cursor end of the selection.
	if res, ok := m.resolveMotion(s, r, m.pending.EffectiveCount()); ok {
		m.pending.Count = 0 // the motion consumed the count
		if res.Kind == motion.Linewise {
			m.cursor = m.buf.ClampCursor(buffer.Position{Line: res.Pos.Line, Col: m.desiredCol})
			// moveTo emits for charwise motions; linewise must emit too so
			// selection listeners track the moving end.
			m.emit(EventCursorMove)
		} else {
			m.moveTo(res.Pos)
		}
		return m, nil
	}

	switch s {
	case "o":
		m.anchor, m.cursor = m.cursor, m.anchor
	case "v":
		m.toggleVisual(Visual)
	case "V":
		m.toggleVisual(mode.VisualLine)
	case "ctrl+v":
		m.toggleVisual(mode.VisualBlock)
	case "d", "x":
		m.visualOperate('d')
	case "y":
		m.visualOperate('y')
	case "c", "s":
		m.visualOperate('c')
	case ">":
		m.visualIndent(1)
	case "<":
		m.visualIndent(-1)
	case "p", "P":
		m.visualPaste(0)
	case "i":
		m.around = false
		m.wait = awaitObject
	case "a":
		m.around = true
		m.wait = awaitObject
	case ":":
		// Remember the selection line bounds for '< / '> and pre-fill the range,
		// mirroring vim's ":'<,'>" when entering the command line from Visual.
		lo, hi := m.anchor.Line, m.cursor.Line
		if lo > hi {
			lo, hi = hi, lo
		}
		m.visualStart, m.visualEnd = lo, hi
		m.mode = Command
		m.cmdline = "'<,'>"
	default:
		_ = hasRune
	}
	return m, nil
}

// visualTextObject sets the selection to the resolved text object: the anchor at
// its start and the cursor on its last rune.
func (m *Model) visualTextObject(r rune) {
	res := m.resolveTextObject(r)
	if !res.OK {
		return
	}
	m.anchor = res.Range.Start
	end := res.Range.End
	if end.Col > 0 {
		end.Col-- // End is exclusive; put the cursor on the last selected rune
	}
	m.cursor = m.buf.ClampCursor(end)
	m.desiredCol = m.cursor.Col
}

// visualIndent shifts the selected lines and leaves visual mode.
func (m *Model) visualIndent(dir int) {
	target := operator.LineTarget(m.anchor.Line, m.cursor.Line)
	m.mode = Normal
	m.indentTarget(target, dir)
}

// visualPaste replaces the selection with reg's contents (vim's visual-mode
// put; reg 0 is the unnamed register), leaving the replaced text in the
// unnamed register.
func (m *Model) visualPaste(reg rune) {
	e := m.regs.Get(reg)
	target := m.visualSelection()
	start := target.Range.Start
	m.mode = Normal
	if e.Text == "" {
		m.runOperator('d', target, m.pending.Register)
		return
	}
	// Delete the selection (which updates the unnamed register), then insert the
	// previously-yanked text at the selection's start.
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
}

// toggleVisual switches to md, or exits to normal mode when already in md.
func (m *Model) toggleVisual(md mode.Mode) {
	if m.mode == md {
		m.mode = Normal
	} else {
		m.mode = md
	}
	// v/V/ctrl+v converts a shift+arrow selection into a sticky vim one.
	m.shiftSelect = false
	// Mode changes alter what the current selection is; keep listeners current.
	m.emit(EventCursorMove)
}

// visualSelection resolves the current selection to an operator Target.
func (m *Model) visualSelection() operator.Target {
	if m.mode == mode.VisualLine {
		return operator.LineTarget(m.anchor.Line, m.cursor.Line)
	}
	// Charwise (block falls back to charwise for now). Inclusive of the cursor cell.
	return operator.Compose(m.buf, m.anchor, m.cursor, motion.Inclusive)
}

// visualOperate applies op to the selection with the pending register and
// leaves visual mode.
func (m *Model) visualOperate(op rune) { m.visualOperateReg(op, m.pending.Register) }

// visualOperateReg applies op to the selection targeting reg explicitly (the
// clipboard actions use `+`) and leaves visual mode.
func (m *Model) visualOperateReg(op, reg rune) {
	target := m.visualSelection()
	m.mode = Normal
	m.runOperator(op, target, reg)
	m.pending.Reset()
}

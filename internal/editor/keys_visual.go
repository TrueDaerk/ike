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
func (m *Model) enterVisual(md mode.Mode) {
	m.mode = md
	m.anchor = m.cursor
}

// updateVisual handles keys while a selection is active. Motions extend the
// selection; d/c/y act on it; v/V/ctrl+v toggle or switch the visual variant.
func (m Model) updateVisual(key tea.KeyPressMsg) (Model, tea.Cmd) {
	s := key.String()
	r, hasRune := firstRune(key)

	if key.Code == tea.KeyEscape {
		m.mode = Normal
		m.wait = awaitNone
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

	// Motions move the cursor end of the selection.
	if res, ok := m.resolveMotion(s, r, 1); ok {
		if res.Kind == motion.Linewise {
			m.cursor = m.buf.ClampCursor(buffer.Position{Line: res.Pos.Line, Col: m.desiredCol})
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
		m.visualPaste()
	case "i":
		m.around = false
		m.wait = awaitObject
	case "a":
		m.around = true
		m.wait = awaitObject
	case ":":
		m.mode = Command
		m.cmdline = ""
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

// visualPaste replaces the selection with the unnamed register's contents
// (vim's visual-mode put), leaving the replaced text in the unnamed register.
func (m *Model) visualPaste() {
	e := m.regs.Get(0)
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
		return
	}
	m.mode = md
}

// visualSelection resolves the current selection to an operator Target.
func (m *Model) visualSelection() operator.Target {
	if m.mode == mode.VisualLine {
		return operator.LineTarget(m.anchor.Line, m.cursor.Line)
	}
	// Charwise (block falls back to charwise for now). Inclusive of the cursor cell.
	return operator.Compose(m.buf, m.anchor, m.cursor, motion.Inclusive)
}

// visualOperate applies op to the selection and leaves visual mode.
func (m *Model) visualOperate(op rune) {
	target := m.visualSelection()
	m.mode = Normal
	m.runOperator(op, target, m.pending.Register)
	m.pending.Reset()
}

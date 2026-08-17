package breakpanel

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/debug"
)

// meta.go is the breakpoint-refinement editing (#1914): condition, hit count
// and log message edit inline on the selected row — `c`, `n` and `l` open a
// one-line editor prefilled with the current value; enter commits (an emptied
// field clears), esc cancels. The panel stays a pure consumer: the commit
// leaves as SetMetaMsg and the root model mutates the store.

// SetMetaMsg asks the root model to replace the refinements of the breakpoint
// on Path:Line (#1914); a zero Meta clears them.
type SetMetaMsg struct {
	Path string
	Line int
	Meta debug.Meta
}

// metaField identifies the refinement being edited.
type metaField int

const (
	fieldCondition metaField = iota
	fieldHit
	fieldLog
)

// label is the editor's prompt and the row-suffix keyword.
func (f metaField) label() string {
	switch f {
	case fieldCondition:
		return "if"
	case fieldHit:
		return "hit"
	default:
		return "log"
	}
}

// Editing reports whether the inline refinement editor is open, so the app
// routes every key to the panel (the debug panel's #627 pattern).
func (m Model) Editing() bool { return m.editing }

// startMetaEdit opens the editor for field f on the selected breakpoint row.
func (m *Model) startMetaEdit(f metaField) {
	r, ok := m.selected()
	if !ok {
		return
	}
	cur := debug.Meta{}
	if m.store != nil {
		cur = m.store.MetaAt(r.path, r.line)
	}
	var s string
	switch f {
	case fieldCondition:
		s = cur.Condition
	case fieldHit:
		s = cur.HitCondition
	default:
		s = cur.LogMessage
	}
	m.editing = true
	m.editField = f
	m.editPath, m.editLine = r.path, r.line
	m.editBuf = []rune(s)
	m.editCur = len(m.editBuf)
}

// cancelEdit closes the editor without committing.
func (m *Model) cancelEdit() {
	m.editing = false
	m.editBuf = nil
	m.editCur = 0
}

// editKey drives the inline editor: printable runes insert, the usual motions
// edit, enter commits the field into the row's refinements, esc cancels.
func (m *Model) editKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.Code {
	case tea.KeyEnter:
		path, line, field, val := m.editPath, m.editLine, m.editField, string(m.editBuf)
		m.cancelEdit()
		meta := debug.Meta{}
		if m.store != nil {
			meta = m.store.MetaAt(path, line)
		}
		switch field {
		case fieldCondition:
			meta.Condition = val
		case fieldHit:
			meta.HitCondition = val
		default:
			meta.LogMessage = val
		}
		msg := SetMetaMsg{Path: path, Line: line, Meta: meta}
		return func() tea.Msg { return msg }
	case tea.KeyEscape:
		m.cancelEdit()
		return nil
	case tea.KeyBackspace:
		if m.editCur > 0 {
			m.editBuf = append(m.editBuf[:m.editCur-1], m.editBuf[m.editCur:]...)
			m.editCur--
		}
		return nil
	case tea.KeyLeft:
		if m.editCur > 0 {
			m.editCur--
		}
		return nil
	case tea.KeyRight:
		if m.editCur < len(m.editBuf) {
			m.editCur++
		}
		return nil
	case tea.KeyHome:
		m.editCur = 0
		return nil
	case tea.KeyEnd:
		m.editCur = len(m.editBuf)
		return nil
	}
	if k.Text != "" {
		runes := []rune(k.Text)
		m.editBuf = append(m.editBuf[:m.editCur], append(runes, m.editBuf[m.editCur:]...)...)
		m.editCur += len(runes)
	}
	return nil
}

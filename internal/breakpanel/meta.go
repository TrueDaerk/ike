package breakpanel

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/debug"
	"ike/internal/ui"
)

// meta.go is the breakpoint-refinement editing (#1914): condition, hit count
// and log message edit inline on the selected row — `c`, `n` and `l` open a
// one-line editor prefilled with the current value; enter commits (an emptied
// field clears), esc cancels. `p` instead asks the root model for the
// breakpoint-properties form, which edits all three fields at once (#2245).
// The panel stays a pure consumer: the commit leaves as SetMetaMsg and the
// root model mutates the store.
//
// Two gates sit in front of the editor (#2245): the field must be one the
// live adapter advertised (Caps), and the typed value must pass the matching
// debug.Validate rule — a rejected value keeps the editor open with the
// reason instead of travelling to an adapter that would answer with an opaque
// setBreakpoints error.

// SetMetaMsg asks the root model to replace the refinements of the breakpoint
// on Path:Line (#1914); a zero Meta clears them.
type SetMetaMsg struct {
	Path string
	Line int
	Meta debug.Meta
}

// EditMetaMsg asks the root model to open the breakpoint-properties form for
// Path:Line (#2245) — the `p` action, the one place where all three fields
// are edited together.
type EditMetaMsg struct {
	Path string
	Line int
}

// NoticeMsg carries a one-line explanation to the root model's notification
// surface (#2245); the panel never notifies itself, like it never mutates the
// store itself.
type NoticeMsg struct {
	Text string
}

// Caps mirrors the adapter capabilities the refinements are gated on (#2245):
// supportsConditionalBreakpoints, supportsHitConditionalBreakpoints and
// supportsLogPoints. Without a live session every field is editable — the
// store is edited offline all the time, and the session strips what its
// adapter cannot honour when the breakpoints are finally pushed.
type Caps struct {
	Condition    bool
	HitCondition bool
	LogMessage   bool
}

// FullCaps is the ungated default (no session, or an adapter supporting all
// three refinements).
func FullCaps() Caps { return Caps{Condition: true, HitCondition: true, LogMessage: true} }

// SetCaps records the live adapter's capabilities; the root model calls it
// when a session starts and when it ends (back to FullCaps).
func (m *Model) SetCaps(c Caps) { m.caps = c }

// allows reports whether field f may be edited under these capabilities.
func (c Caps) allows(f metaField) bool {
	switch f {
	case fieldCondition:
		return c.Condition
	case fieldHit:
		return c.HitCondition
	default:
		return c.LogMessage
	}
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

// unsupportedNotice is the wording for a field the adapter did not advertise.
func (f metaField) unsupportedNotice() string {
	switch f {
	case fieldCondition:
		return "debug: adapter does not support conditional breakpoints"
	case fieldHit:
		return "debug: adapter does not support hit counts"
	default:
		return "debug: adapter does not support logpoints"
	}
}

// startMetaEdit opens the editor for field f on the selected breakpoint row.
// A field the live adapter cannot honour stays closed and answers with the
// notice command instead (#2245) — the session is never handed a refinement
// it would have to reject.
func (m *Model) startMetaEdit(f metaField) tea.Cmd {
	r, ok := m.selected()
	if !ok {
		return nil
	}
	if !m.caps.allows(f) {
		notice := NoticeMsg{Text: f.unsupportedNotice()}
		return func() tea.Msg { return notice }
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
	m.editErr = ""
	return nil
}

// cancelEdit closes the editor without committing.
func (m *Model) cancelEdit() {
	m.editing = false
	m.editBuf = nil
	m.editCur = 0
	m.editErr = ""
}

// editKey drives the inline editor: printable runes insert, the usual motions
// edit, enter commits the field into the row's refinements, esc cancels.
func (m *Model) editKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.Code {
	case tea.KeyEnter:
		path, line, field, val := m.editPath, m.editLine, m.editField, string(m.editBuf)
		if err := field.validate(val); err != nil {
			// A rejected value keeps the editor open with the reason, like
			// the run-configuration form's row editor (#2245).
			m.editErr = err.Error()
			return nil
		}
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
	}
	// Everything else is shared line editing (#2002): a movable cursor with
	// word motions, word/line kills, the macOS opt/cmd chords and rune-safe
	// backspace, plus printable insertion at the cursor.
	if out, ncur, handled, _ := ui.EditKey(k, string(m.editBuf), m.editCur); handled {
		m.editBuf, m.editCur = []rune(out), ncur
		m.editErr = "" // the next keystroke retires the complaint
	}
	return nil
}

// validate applies the field's input rule (#2245); the rules themselves live
// next to the store, so the form and the inline editor reject alike.
func (f metaField) validate(val string) error {
	switch f {
	case fieldCondition:
		return debug.ValidateCondition(val)
	case fieldHit:
		return debug.ValidateHitCondition(val)
	default:
		return debug.ValidateLogMessage(val)
	}
}

// PasteText inserts a pasted block into the open inline editor at its cursor
// (#2002); it reports whether anything was consumed, so a closed editor lets
// the paste fall through.
func (m *Model) PasteText(text string) bool {
	if !m.editing {
		return false
	}
	out, ncur, changed := ui.PasteText(string(m.editBuf), m.editCur, text)
	if !changed {
		return false
	}
	m.editBuf, m.editCur = []rune(out), ncur
	return true
}

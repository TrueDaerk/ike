package settings

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// textfield.go is the shared settings text input (0420, #888): every inline
// edit routes through ui.Field — a movable cursor, home/end, word motions and
// word/line kills, and rune-safe backspace (the nine hand-rolled append-only
// inputs byte-sliced backspace and corrupted multi-byte text) — rendered with
// ui.CursorView.
//
// Since #2459 the state itself is ui.Field; textField only adds the
// settings-local constructors (and the older Handle name its call sites use).

// textField is one single-line input's state.
type textField struct {
	ui.Field
}

// newTextField starts with initial and the cursor at its end.
func newTextField(initial string) textField {
	return textField{Field: ui.NewField(initial)}
}

// newTextFieldAt starts with initial and an explicit cursor (forms that keep
// one cursor across several field strings).
func newTextFieldAt(initial string, cur int) textField {
	f := textField{Field: ui.Field{Text: initial, Cur: cur}}
	if f.Cur > f.Len() {
		f.Cur = f.Len()
	}
	return f
}

// Handle applies one key through ui.EditKey. handled reports the key was an
// editing key; changed that the text differs.
func (f *textField) Handle(key tea.KeyPressMsg) (handled, changed bool) {
	return f.Key(key)
}

// filterKey applies one key to a page's live filter through ui.EditKey
// (#2002). The filter columns keep their text in a plain string (every
// renderer and matcher reads it) plus a rune cursor, so this is the seam that
// gives them the same editing as textField: a movable cursor, word motions,
// word/line kills, the macOS opt/cmd chords, and rune-safe backspace.
func filterKey(key tea.KeyPressMsg, filter *string, cur *int) (handled, changed bool) {
	f := ui.Field{Text: *filter, Cur: *cur}
	handled, changed = f.Key(key)
	if handled {
		*filter, *cur = f.Text, f.Cur
	}
	return handled, changed
}

// filterPaste inserts a pasted block into a filter at its cursor.
func filterPaste(text string, filter *string, cur *int) (changed bool) {
	f := ui.Field{Text: *filter, Cur: *cur}
	if changed = f.Paste(text); changed {
		*filter, *cur = f.Text, f.Cur
	}
	return changed
}

// filterView renders a filter with its cursor cell.
func filterView(filter string, cur int) string { return ui.CursorView(filter, cur) }

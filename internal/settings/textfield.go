package settings

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// textfield.go is the shared settings text input (0420, #888): every inline
// edit routes through ui.EditKey — a movable cursor, home/end, word motions
// and word deletes, and rune-safe backspace (the nine hand-rolled append-only
// inputs byte-sliced backspace and corrupted multi-byte text) — rendered with
// ui.CursorView.

// textField is one single-line input's state.
type textField struct {
	text string
	cur  int
}

// newTextField starts with initial and the cursor at its end.
func newTextField(initial string) textField {
	return textField{text: initial, cur: len([]rune(initial))}
}

// newTextFieldAt starts with initial and an explicit cursor (forms that keep
// one cursor across several field strings).
func newTextFieldAt(initial string, cur int) textField {
	f := textField{text: initial, cur: cur}
	if f.cur > len([]rune(initial)) {
		f.cur = len([]rune(initial))
	}
	return f
}

// Set replaces the text, cursor at the end.
func (f *textField) Set(text string) {
	f.text = text
	f.cur = len([]rune(text))
}

// Handle applies one key through ui.EditKey. handled reports the key was an
// editing key; changed that the text differs.
func (f *textField) Handle(key tea.KeyPressMsg) (handled, changed bool) {
	out, cur, handled, changed := ui.EditKey(key, f.text, f.cur)
	if handled {
		f.text, f.cur = out, cur
	}
	return handled, changed
}

// View renders the text with the cursor cell.
func (f textField) View() string { return ui.CursorView(f.text, f.cur) }

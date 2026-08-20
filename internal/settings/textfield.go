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

// Paste inserts a pasted block at the cursor (#1273). changed is false when
// the block flattened to nothing.
func (f *textField) Paste(text string) (changed bool) {
	out, cur, changed := ui.PasteText(f.text, f.cur, text)
	if changed {
		f.text, f.cur = out, cur
	}
	return changed
}

// View renders the text with the cursor cell.
func (f textField) View() string { return ui.CursorView(f.text, f.cur) }

// filterKey applies one key to a page's live filter through ui.EditKey
// (#2002). The filter columns keep their text in a plain string (every
// renderer and matcher reads it) plus a rune cursor, so this is the seam that
// gives them the same editing as textField: a movable cursor, word motions,
// word/line kills, the macOS opt/cmd chords, and rune-safe backspace.
func filterKey(key tea.KeyPressMsg, filter *string, cur *int) (handled, changed bool) {
	out, ncur, handled, changed := ui.EditKey(key, *filter, *cur)
	if handled {
		*filter, *cur = out, ncur
	}
	return handled, changed
}

// filterPaste inserts a pasted block into a filter at its cursor.
func filterPaste(text string, filter *string, cur *int) (changed bool) {
	out, ncur, changed := ui.PasteText(*filter, *cur, text)
	if changed {
		*filter, *cur = out, ncur
	}
	return changed
}

// filterView renders a filter with its cursor cell.
func filterView(filter string, cur int) string { return ui.CursorView(filter, cur) }

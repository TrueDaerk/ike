package ui

// field.go is the value type every single-line input should hold (#2459).
//
// EditKey, PasteText and CursorView are the behaviour; Field is the state
// they operate on. Before it, each of the ~60 one-line inputs in the IDE
// carried its own `text string` + `cur int` pair and its own four-line
// "call EditKey, store the result if handled" dance — the same boilerplate
// written sixty times, and sixty places where a caller could forget to write
// the cursor back, forget to route paste, or render without a cursor cell.
//
// A Field is a plain struct with exported fields, deliberately: a host that
// has to read the text for a matcher, a renderer or a completion source just
// reads Text, and one that seeds a cursor writes Cur. The zero value is an
// empty field with the cursor at position 0.

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Field is one single-line text input's state: the text and the caret, as a
// rune index into it.
type Field struct {
	Text string
	Cur  int
}

// NewField returns a field holding text with the cursor at its end.
func NewField(text string) Field {
	return Field{Text: text, Cur: len([]rune(text))}
}

// Key applies one editing key through EditKey. handled reports the key was an
// editing key (an unhandled key is the caller's to interpret); changed reports
// the text actually differs, which is the signal to re-filter a list, re-run a
// preview or refresh completions.
//
// Caller chords win: run the field's own bindings first and hand Key what is
// left over.
func (f *Field) Key(msg tea.KeyPressMsg) (handled, changed bool) {
	out, cur, handled, changed := EditKey(msg, f.Text, f.Cur)
	if handled {
		f.Text, f.Cur = out, cur
	}
	return handled, changed
}

// Paste inserts a pasted block at the cursor through PasteText, reporting
// whether anything changed (a block that flattens to nothing does not).
func (f *Field) Paste(paste string) (changed bool) {
	out, cur, changed := PasteText(f.Text, f.Cur, paste)
	if changed {
		f.Text, f.Cur = out, cur
	}
	return changed
}

// View renders the text with the reverse-video cursor cell.
func (f Field) View() string { return CursorView(f.Text, f.Cur) }

// ViewSel renders the text like View, with the rune range [selStart, selEnd)
// painted in selStyle — a preselected prefill the cursor still sits inside.
func (f Field) ViewSel(selStart, selEnd int, selStyle lipgloss.Style) string {
	return CursorViewSel(f.Text, f.Cur, selStart, selEnd, selStyle)
}

// Set replaces the text and puts the cursor at its end.
func (f *Field) Set(text string) {
	f.Text = text
	f.Cur = len([]rune(text))
}

// Clear empties the field.
func (f *Field) Clear() { f.Text, f.Cur = "", 0 }

// Empty reports whether the field holds no text.
func (f Field) Empty() bool { return f.Text == "" }

// Runes is the text as runes — what a host slicing around the caret needs,
// since Cur is a rune index and byte slicing would corrupt multi-byte text.
func (f Field) Runes() []rune { return []rune(f.Text) }

// Len is the text's length in runes, i.e. the cursor's maximum position.
func (f Field) Len() int { return len([]rune(f.Text)) }

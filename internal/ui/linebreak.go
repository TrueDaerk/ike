package ui

// linebreak.go lets a *single-line* field hold a line break (#2600).
//
// The find/replace inputs are the one place where a one-row input has to carry
// a break: "replace every ';' with ';' + newline" and "find foo at the end of a
// line followed by bar" are ordinary editing tasks, and JetBrains answers both
// with opt+enter in the Find/Replace fields. Everything else in the tree — the
// palette query, a file-name prompt, the jq program line — must keep rejecting
// breaks, so this is deliberately *not* folded into EditKey: a host that wants
// the chord binds IsBreakKey itself, ahead of Field.Key, the same way the
// replace panel already owns ctrl+u.
//
// The break is stored as a real '\n' in the field text, so the cursor, the word
// motions and backspace all treat it as exactly one rune for free; only
// rendering is special-cased, through ShowBreaks below, which is what keeps the
// field one row tall.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// BreakMarker is the glyph a line break renders as inside a one-line field.
// One cell wide, so the cursor columns after it stay honest.
const BreakMarker = "⏎"

// IsBreakKey reports whether msg is the insert-a-line-break chord, alt+enter
// (opt+enter on macOS). shift is tolerated, like the chords in EditKey.
func IsBreakKey(msg tea.KeyPressMsg) bool {
	return msg.Code == tea.KeyEnter && msg.Mod&^tea.ModShift == tea.ModAlt
}

// InsertBreak inserts a line break into text at rune cursor cur (clamped into
// range first), returning the new text and cursor.
func InsertBreak(text string, cur int) (out string, ncur int) {
	r := []rune(text)
	if cur < 0 {
		cur = 0
	}
	if cur > len(r) {
		cur = len(r)
	}
	return string(r[:cur]) + "\n" + string(r[cur:]), cur + 1
}

// InsertBreak inserts a line break at the field's cursor.
func (f *Field) InsertBreak() { f.Text, f.Cur = InsertBreak(f.Text, f.Cur) }

// HasBreak reports whether the field holds a line break.
func (f Field) HasBreak() bool { return strings.Contains(f.Text, "\n") }

// ShowBreaks renders a one-line field's text with every line break replaced by
// the dimmed marker glyph, so the row stays one row and the break stays
// visible. It is what a host renders instead of the raw text — CursorView and
// CursorViewSel already route through it.
func ShowBreaks(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	return strings.ReplaceAll(s, "\n", breakGlyph())
}

// breakGlyph is the dimmed marker.
func breakGlyph() string { return lipgloss.NewStyle().Faint(true).Render(BreakMarker) }

// cellGlyph is the one-cell rendering of a rune sitting under the cursor: a
// line break shows its marker, anything else itself.
func cellGlyph(c rune) string {
	if c == '\n' {
		return BreakMarker
	}
	return string(c)
}

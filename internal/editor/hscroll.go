package editor

// hscroll.go — the editor's half of the shared horizontal-scroll indicator
// (#2377). A horizontally scrolled buffer used to look exactly like an
// unscrolled one: the gutter kept showing line numbers, "Ln x, Col y" spoke
// about the cursor, and nothing said the text was shifted. The marks close
// that gap without costing a row or a column: "‹" replaces the text area's
// leftmost cell whenever view.Left is past 0, "›" its rightmost whenever the
// rendered line still had content when the width budget ran out.
//
// Both are overlays, like the vertical scrollbar's column: cursor cells, mouse
// hit zones and the status line's column report all keep counting the same
// cells they did before. Under soft wrap there is no horizontal scroll (#64)
// and no line runs past the edge, so nothing renders.

import (
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/hscroll"
)

// hMarkWidth is the window the edge marks measure against: the text area minus
// the column the vertical scrollbar overlays, so the right mark never renders
// underneath the bar (the same reservation annotations and cursor following
// make, #1728/#1827).
func (m Model) hMarkWidth(textWidth int) int {
	if _, _, _, _, ok := m.scrollbarGeometry(); ok && textWidth > 1 {
		return textWidth - 1
	}
	return textWidth
}

// stampHScroll overlays the edge marks on one rendered line body. over is the
// renderer's own verdict that the line continued past the window; a body wider
// than the mark window (the scrollbar's column) counts too, so the right mark
// stays truthful when the bar shortens the window.
//
// A mark covers the cell it lands on, so the one cell it may never take is the
// caret's: the cursor has to stay visible where the user put it. On the row
// the caret sits on, a mark sharing its cell yields — every other visible row
// still carries it, so the scroll state itself never disappears.
func (m Model) stampHScroll(body string, line, textWidth int, over bool) string {
	if !m.hMarks || m.softWrap {
		return body
	}
	w := m.hMarkWidth(textWidth)
	left := m.view.Left > 0
	right := over || ansi.StringWidth(body) > w
	if !left && !right {
		return body
	}
	switch m.caretCell(line, w) {
	case 0:
		left = false
	case w - 1:
		right = false
	}
	return hscroll.Stamp(body, w, left, right, m.hMarkStyle())
}

// caretCell returns the primary caret's display cell inside the mark window on
// line, or -1 when the caret is elsewhere: another line, an unfocused pane, or
// scrolled out of the window. A caret off the left edge must not be mistaken
// for one sitting on the first cell — DisplayOffset floors at 0 either way.
func (m Model) caretCell(line, w int) int {
	if !m.focused || line != m.cursor.Line {
		return -1
	}
	col := m.cursor.Col
	if m.svActive() {
		// sv rows scroll in display space (#1724), so view.Left compares
		// against the caret's expanded column, not its rune column.
		col = m.svDisplayCol(line, col)
	}
	if col < m.view.Left {
		return -1
	}
	d := m.DisplayOffset(line, m.cursor.Col)
	if d < 0 || d >= w {
		return -1
	}
	return d
}

// hMarkStyle paints the marks in the scrollbar thumb's tone: the slot that
// already means "this is scroll state, not content" everywhere else in the
// pane, so the marks re-theme with the bar instead of carrying a fixed colour.
func (m Model) hMarkStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.theme().ScrollbarThumb)
}

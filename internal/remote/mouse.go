package remote

// mouse.go is the browser pane's mouse control (#2259). The pane was
// keyboard-only: neither the wheel nor a click reached it, while its two
// models — the archive viewer it borrows its tree shape from and the explorer
// it borrows its keys from — have scrolled and selected with the mouse since
// #1852 / #1040.
//
// Coordinates are pane-content-local, matching what View draws: y 0 is the
// header line, the rows are y 1 … bodyHeight, the key hints occupy the last
// line. The wheel, the hit-test and the double-click clock come from the
// shared list-mouse layer.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// headerRows is how many lines sit above the first entry row.
const headerRows = 1

// Wheel scrolls the entry list by delta rows (positive = down). The scroll
// clamps at both ends — the last page never scrolls past its final row — and
// the cursor is dragged along so it stays inside the visible window, which is
// the invariant clampScroll maintains for the keyboard.
func (m *Model) Wheel(delta int) {
	if m.connErr != nil {
		return // the whole pane is a connect-failure notice; nothing scrolls
	}
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows), m.bodyHeight())
}

// Click handles one left press at content-local (x, y): the row under the
// pointer becomes the selection, a press on a directory's fold glyph expands
// or collapses it right away (scanning it on first expand), and a second
// press on the same row within the double-click window activates it — opening
// a file read-only, folding a directory — exactly like enter. Presses on the
// header, the footer or empty space are ignored.
func (m *Model) Click(x, y int) tea.Cmd {
	if m.connErr != nil {
		return nil
	}
	i, ok := ui.RowAt(y, m.top, headerRows, m.bodyHeight(), len(m.rows))
	if !ok || x < 0 {
		m.clicks.Reset()
		return nil
	}
	m.cursor = i
	r := m.rows[i]
	if r.entry.IsDir() && onFoldGlyph(r, x) {
		// The fold glyph answers to a single click, like the explorer's caret.
		m.clicks.Reset()
		return m.activate()
	}
	if m.clicks.Double(i, m.clock()) {
		// Clear the pending click after an activation, so a third click does
		// not activate again.
		m.clicks.Reset()
		return m.activate()
	}
	return nil
}

// onFoldGlyph reports whether x lies on the two-cell fold glyph of row r. The
// row is rendered as one leading space, two indent cells per depth level, then
// the glyph — see renderRow.
func onFoldGlyph(r row, x int) bool {
	start := 1 + 2*r.depth
	return x >= start && x < start+2
}

// clock reads the injectable clock; a pane built without one falls back to
// the wall clock.
func (m *Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

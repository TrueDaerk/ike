package archview

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Mouse control (#1852), mirroring the explorer tree (#1040) and the list
// panels (#1024/#1155). Coordinates are pane-content-local: y 0 is the header
// line, the rows start at y 1, the key hints occupy the last line.

// doubleClickWindow is the maximum delay between two clicks on the same row
// for the second to activate it, matching the explorer.
const doubleClickWindow = 400 * time.Millisecond

// Wheel scrolls the entry list by delta rows (positive = down). The scroll
// clamps at both ends — the last page never scrolls past its final row — and
// the cursor is dragged along so it stays inside the visible window, which is
// the invariant clampScroll maintains for the keyboard.
func (m *Model) Wheel(delta int) {
	h := m.bodyHeight()
	maxTop := len(m.rows) - h
	if maxTop < 0 {
		maxTop = 0
	}
	m.top += delta
	if m.top > maxTop {
		m.top = maxTop
	}
	if m.top < 0 {
		m.top = 0
	}
	if m.cursor < m.top {
		m.cursor = m.top
	}
	if m.cursor >= m.top+h {
		m.cursor = m.top + h - 1
	}
	if m.cursor > len(m.rows)-1 {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// Click handles one left press at content-local (x, y): the row under the
// pointer becomes the selection, a press on a directory's fold glyph toggles
// it right away, and a second press on the same row within doubleClickWindow
// activates it — opening a file read-only, folding a directory — exactly like
// enter. Presses on the header, the footer or empty space are ignored.
func (m *Model) Click(x, y int) tea.Cmd {
	i := m.top + (y - 1)
	if x < 0 || y < 1 || y > m.bodyHeight() || i < 0 || i >= len(m.rows) {
		return nil
	}
	m.cursor = i
	r := m.rows[i]
	if r.isDir && onFoldGlyph(r, x) {
		// The fold glyph answers to a single click, like the explorer's caret.
		m.resetClick()
		m.toggleDir()
		return nil
	}
	if m.doubleClicked(i) {
		m.resetClick()
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

// doubleClicked records one click on a row and reports whether it completes a
// double-click on that row.
func (m *Model) doubleClicked(i int) bool {
	at := m.clock()
	hit := m.lastClickRow == i && at.Sub(m.lastClickAt) <= doubleClickWindow
	m.lastClickRow, m.lastClickAt = i, at
	return hit
}

// resetClick clears the pending single-click state after an activation, so a
// third click does not activate again.
func (m *Model) resetClick() {
	m.lastClickRow = -1
	m.lastClickAt = time.Time{}
}

// clock reads the injectable clock; the zero Model (a moved-out pane slot)
// has none, so it falls back to the wall clock.
func (m *Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

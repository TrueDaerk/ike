package scratchpanel

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Mouse control, mirroring the Problems panel (#514/#1024). Coordinates are
// pane-content-local; unlike Problems the panel has no header line, so row 0
// is the first scratch.

// doubleClickWindow is the maximum delay between two clicks on the same row
// for the second to activate it, matching the explorer.
const doubleClickWindow = 400 * time.Millisecond

// Wheel scrolls the list by delta rows (positive = down); the cursor follows
// so it stays inside the visible window.
func (m *Model) Wheel(delta int) {
	maxTop := len(m.entries) - 1
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
	if h := m.bodyHeight(); m.cursor >= m.top+h {
		m.cursor = m.top + h - 1
	}
	if m.cursor > len(m.entries)-1 {
		m.cursor = len(m.entries) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// Click handles one left click at content-local (x, y): a row click selects,
// a second click on the selected row within doubleClickWindow opens it. A
// click while a delete confirmation is armed cancels it — the prompt only
// ever answers to y/n keys.
func (m *Model) Click(x, y int) tea.Cmd {
	if m.confirm != "" {
		m.confirm = ""
		return nil
	}
	i := m.top + y
	if y < 0 || y >= m.bodyHeight() || i < 0 || i >= len(m.entries) {
		return nil
	}
	double := m.doubleClicked(i)
	m.cursor = i
	if double {
		return m.activate(i)
	}
	return nil
}

// doubleClicked records one click on a row and reports whether it completes
// a double-click on that row.
func (m *Model) doubleClicked(row int) bool {
	nowAt := m.now()
	hit := m.lastClickRow == row && nowAt.Sub(m.lastClickAt) <= doubleClickWindow
	m.lastClickRow, m.lastClickAt = row, nowAt
	return hit
}

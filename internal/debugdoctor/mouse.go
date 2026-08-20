package debugdoctor

import (
	tea "charm.land/bubbletea/v2"
)

// Mouse control, mirroring the Breakpoints panel. Coordinates are
// pane-content-local: y 0-1 are the header/status lines, rows start at y 2.

// Wheel scrolls the trace by delta rows (positive = down); the cursor is
// dragged along so it stays inside the visible window.
func (m *Model) Wheel(delta int) {
	n := len(m.entries())
	maxTop := n - 1
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
	if m.cursor > n-1 {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// Click selects the row under content-local (x, y).
func (m *Model) Click(x, y int) tea.Cmd {
	i := m.top + (y - 2)
	if y < 2 || y >= 2+m.bodyHeight() || i < 0 || i >= len(m.entries()) {
		return nil
	}
	m.cursor = i
	return nil
}

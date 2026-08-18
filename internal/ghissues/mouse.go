package ghissues

// mouse.go: wheel scroll plus click-select / double-click-open, mirroring the
// Usages panel (#514): activating a row needs a second click on the same row
// within doubleClickWindow.

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// doubleClickWindow bounds the second click of a double-click.
const doubleClickWindow = 400 * time.Millisecond

// Wheel scrolls the list (or the detail view) by delta rows.
func (m *Model) Wheel(delta int) {
	if m.detail {
		m.detailTop += delta
		m.clampDetail()
		return
	}
	m.cursor += delta
	m.clampScroll()
}

// Click handles one left click at pane-local (x, y): a body click selects the
// row, a second click on the same row within the window opens its detail.
func (m *Model) Click(x, y int) tea.Cmd {
	if m.detail {
		return nil
	}
	row := m.top + y - 1 // header line
	if y < 1 || row < 0 || row >= len(m.visible) {
		return nil
	}
	m.cursor = row
	m.clampScroll()
	if row == m.lastClickRow && m.now().Sub(m.lastClickAt) <= doubleClickWindow {
		m.lastClickRow, m.lastClickAt = -1, time.Time{}
		m.openDetail()
		return nil
	}
	m.lastClickRow, m.lastClickAt = row, m.now()
	return nil
}

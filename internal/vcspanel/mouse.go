package vcspanel

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// doubleClickWindow is the maximum delay between two clicks on the same row
// for the second to activate it (#514), matching the explorer.
const doubleClickWindow = 400 * time.Millisecond

// doubleClicked records one click on a row and reports whether it completes
// a double-click on that row.
func (m *Model) doubleClicked(row int) bool {
	nowAt := m.now()
	hit := m.lastClickRow == row && nowAt.Sub(m.lastClickAt) <= doubleClickWindow
	m.lastClickRow, m.lastClickAt = row, nowAt
	return hit
}

// Mouse control (#503). Coordinates are pane-content-local: y 0 is the
// header, the list body starts at y 1.

// Wheel scrolls the list by delta rows (positive = down).
func (m *Model) Wheel(delta int) {
	m.chTop = clampInt(m.chTop+delta, 0, maxInt(0, len(m.chRows)-1))
	m.chCursor = clampInt(m.chCursor, m.chTop, maxInt(m.chTop, m.chTop+m.bodyHeight()-1))
}

// Click handles one left click at content-local (x, y): a row click selects,
// a second click on the same row within the double-click window opens the
// file's diff against HEAD (#514).
func (m *Model) Click(x, y int) tea.Cmd {
	if y == 0 {
		return nil // header
	}
	i := m.chTop + (y - 1)
	if i < 0 || i >= len(m.chRows) {
		return nil
	}
	double := m.doubleClicked(i)
	m.chCursor = i
	if double {
		path := m.chRows[i].Path
		return func() tea.Msg { return OpenDiffMsg{Path: path} }
	}
	return nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

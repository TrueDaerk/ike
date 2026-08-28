package vcspanel

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// Mouse control (#503). Coordinates are pane-content-local: y 0 is the
// header, the list body starts at y 1. The wheel, the hit-test and the
// double-click clock come from the shared list-mouse layer (#2259).

// headerRows is how many lines sit above the first changes row.
const headerRows = 1

// Wheel scrolls the list by delta rows (positive = down); the cursor is
// dragged along so it stays inside the visible window.
func (m *Model) Wheel(delta int) {
	ui.WheelWindow(&m.chTop, &m.chCursor, delta, len(m.chRows), m.bodyHeight())
}

// Click handles one left click at content-local (x, y): a row click selects,
// a second click on the same row within the double-click window opens the
// file's diff against HEAD (#514).
func (m *Model) Click(x, y int) tea.Cmd {
	i, ok := ui.RowAt(y, m.chTop, headerRows, m.bodyHeight(), len(m.chRows))
	if !ok {
		m.clicks.Reset()
		return nil
	}
	double := m.clicks.Double(i, m.now())
	m.chCursor = i
	if double {
		m.clicks.Reset()
		path := m.chRows[i].Path
		return func() tea.Msg { return OpenDiffMsg{Path: path} }
	}
	return nil
}

package testresults

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// Mouse control, mirroring the Problems panel. Coordinates are
// pane-content-local: y 0 is the header line, rows start at y 1. The wheel,
// the hit-test and the double-click clock come from the shared list-mouse
// layer (#2259).

// headerRows is how many lines sit above the first tree row.
const headerRows = 1

// Wheel scrolls by delta rows (positive = down): the detail column when it
// holds the focus, the tree otherwise (the cursor dragged along so it stays
// inside the visible window).
func (m *Model) Wheel(delta int) {
	if m.detailFocus {
		m.detailTop += delta
		m.clampDetail()
		return
	}
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows), m.bodyHeight())
}

// Click handles one left click at content-local (x, y). A click right of the
// separator focuses the detail column; a tree-row click selects, a second
// click on the selected row within the double-click window activates it.
func (m *Model) Click(x, y int) tea.Cmd {
	tw, _ := m.colWidths()
	if x > tw {
		m.detailFocus = true
		m.clicks.Reset()
		return nil
	}
	m.detailFocus = false
	i, ok := ui.RowAt(y, m.top, headerRows, m.bodyHeight(), len(m.rows))
	if !ok {
		m.clicks.Reset()
		return nil
	}
	double := m.clicks.Double(i, m.now())
	m.cursor = i
	if double {
		m.clicks.Reset()
		return m.activate(i)
	}
	return nil
}

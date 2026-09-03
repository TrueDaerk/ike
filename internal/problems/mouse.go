package problems

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// Mouse control, mirroring the VCS panel (#503/#514). Coordinates are
// pane-content-local: y 0 is the header line, y 1 the filter row (#2156), and
// list rows start at y 2. The wheel, the hit-test and the double-click clock
// come from the shared list-mouse layer (#2259) so every list pane behaves
// the same.

// headerRows is how many lines sit above the first list row: the title and
// the filter row.
const headerRows = 2

// Wheel scrolls the list by delta rows (positive = down); the cursor is
// dragged along so it stays inside the visible window.
func (m *Model) Wheel(delta int) {
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows), m.bodyHeight())
}

// Click handles one left click at content-local (x, y): a row click selects,
// a second click on the selected row within the double-click window activates
// it.
func (m *Model) Click(x, y int) tea.Cmd {
	return m.clicks.ClickRow(y, m.top, headerRows, m.bodyHeight(), len(m.rows), m.now(), &m.cursor, m.activate)
}

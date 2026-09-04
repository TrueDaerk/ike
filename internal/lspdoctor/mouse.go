package lspdoctor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// Mouse control, mirroring the Xdebug Doctor. Coordinates are
// pane-content-local: y 0-1 are the header/status lines, rows start at y 2.
// The wheel and the hit-test come from the shared list-mouse layer (#2259).
// The report has no enter action, so a row click only selects — there is
// nothing for a double click to activate.

// headerRows is how many lines sit above the first report row.
const headerRows = 2

// Wheel scrolls the report by delta rows (positive = down); the cursor is
// dragged along so it stays inside the visible window.
func (m *Model) Wheel(delta int) {
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows()), m.bodyHeight())
}

// Click selects the row under content-local (x, y).
func (m *Model) Click(x, y int) tea.Cmd {
	ui.SelectClick(y, m.top, headerRows, m.bodyHeight(), len(m.rows()), &m.cursor)
	return nil
}

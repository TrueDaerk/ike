package debugdoctor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// Mouse control, mirroring the Breakpoints panel. Coordinates are
// pane-content-local: y 0-1 are the header/status lines, rows start at y 2.
// The wheel and the hit-test come from the shared list-mouse layer (#2259).
// The trace has no enter action, so a row click only selects — there is
// nothing for a double click to activate.

// headerRows is how many lines sit above the first trace row.
const headerRows = 2

// Wheel scrolls the trace by delta rows (positive = down); the cursor is
// dragged along so it stays inside the visible window.
func (m *Model) Wheel(delta int) {
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.entries()), m.bodyHeight())
}

// Click selects the row under content-local (x, y).
func (m *Model) Click(x, y int) tea.Cmd {
	if i, ok := ui.RowAt(y, m.top, headerRows, m.bodyHeight(), len(m.entries())); ok {
		m.cursor = i
	}
	return nil
}

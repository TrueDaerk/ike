package breakpanel

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// Mouse control, mirroring the Problems panel (#1024). Coordinates are
// pane-content-local: y 0 is the header line, rows start at y 1. The wheel,
// the hit-test and the double-click clock come from the shared list-mouse
// layer (#2259).

// headerRows is how many lines sit above the first list row.
const headerRows = 1

// glyphCol is the column holding a breakpoint row's enable/disable checkbox.
const glyphCol = 3

// Wheel scrolls the list by delta rows (positive = down); the cursor is
// dragged along so it stays inside the visible window.
func (m *Model) Wheel(delta int) {
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows), m.bodyHeight())
}

// Click handles one left click at content-local (x, y): a row click selects —
// a click on a breakpoint row's glyph cell flips its enabled state — and a
// second click on the selected row within the double-click window jumps to it.
func (m *Model) Click(x, y int) tea.Cmd {
	// A click while the refinement editor is open cancels the edit first and
	// then selects normally (the debug panel's #639 rule).
	m.cancelEdit()
	i, ok := ui.RowAt(y, m.top, headerRows, m.bodyHeight(), len(m.rows))
	if !ok {
		m.clicks.Reset()
		return nil
	}
	double := m.clicks.Double(i, m.now())
	m.cursor = i
	r := m.rows[i]
	// The glyph column is the enable/disable checkbox of the row.
	if !r.header && x == glyphCol && !double {
		msg := ToggleEnabledMsg{Path: r.path, Line: r.line}
		return func() tea.Msg { return msg }
	}
	if double {
		m.clicks.Reset()
		return m.activate(i)
	}
	return nil
}

package debugpanel

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Mouse control (#626), mirroring the vcs panel. Coordinates are
// pane-content-local: y 0 is the column-title row, list rows start at y 1; the
// column is chosen by x against the separator, matching View's layout.

// doubleClickWindow is the maximum delay between two clicks on the same row for
// the second to activate it, matching the explorer and vcs panel.
const doubleClickWindow = 400 * time.Millisecond

// clock returns the injectable time source, defaulting to time.Now so a
// zero-value Model (not built via New) still works.
func (m *Model) clock() time.Time {
	if m.now == nil {
		return time.Now()
	}
	return m.now()
}

// doubleClicked records a click on (col, row) and reports whether it completes
// a double-click there.
func (m *Model) doubleClicked(col column, row int) bool {
	at := m.clock()
	hit := m.lastClickCol == col && m.lastClickRow == row &&
		at.Sub(m.lastClickAt) <= doubleClickWindow
	m.lastClickCol, m.lastClickRow, m.lastClickAt = col, row, at
	return hit
}

// columnAt maps a content-local x to the column under it, matching View's
// three-column split (frames │ variables │ output).
func (m Model) columnAt(x int) column {
	fw, vw, _ := m.colWidths()
	switch {
	case x < fw:
		return colFrames
	case x < fw+1+vw:
		return colVars
	default:
		return colOutput
	}
}

// Wheel scrolls the focused column by delta rows (positive = down). It works
// in every panel state (#637): the OUTPUT column keeps streaming while the
// debuggee runs, and scrolling it toggles auto-follow (see scrollOutput).
func (m *Model) Wheel(delta int) {
	switch m.col {
	case colFrames:
		m.frameTop = clamp(m.frameTop+delta, 0, max(0, len(m.frames)-m.bodyHeight()))
	case colOutput:
		m.scrollOutput(delta)
	default:
		m.varTop = clamp(m.varTop+delta, 0, max(0, len(m.flat())-m.bodyHeight()))
	}
}

// Click handles one left click at content-local (x, y): it focuses the column
// under the cursor, selects the row, and activates it on a double-click (frame
// select / variable expand-collapse) — mirroring enter.
func (m *Model) Click(x, y int) tea.Cmd {
	if y == 0 { // the title row has nothing to click
		return nil
	}
	switch m.columnAt(x) {
	case colFrames:
		i := m.frameTop + (y - 1)
		if i < 0 || i >= len(m.frames) {
			return nil
		}
		m.col = colFrames
		m.frameSel = i
		if m.doubleClicked(colFrames, i) {
			return m.activate()
		}
	case colOutput:
		// Clicking the output column only focuses it (for wheel/keys); output
		// rows have no activation.
		m.col = colOutput
	default:
		rows := m.flat()
		i := m.varTop + (y - 1)
		if i < 0 || i >= len(rows) {
			return nil
		}
		m.col = colVars
		m.varSel = i
		if m.doubleClicked(colVars, i) {
			return m.activate()
		}
	}
	return nil
}

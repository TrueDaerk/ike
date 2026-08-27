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
// two-column split (frames │ variables).
func (m Model) columnAt(x int) column {
	fw, _ := m.colWidths()
	if x < fw {
		return colFrames
	}
	return colVars
}

// Wheel scrolls the focused column by delta rows (positive = down).
// Frames/vars drag the selection along so it stays inside the visible window
// (#639, matching the vcs panel) — except while the inline value editor is
// open: then the wheel only scrolls, because moving varSel would re-anchor the
// editor onto a different row (#627).
func (m *Model) Wheel(delta int) {
	if m.ConsoleActive() {
		return // the console's wheel routes to the terminal app-side (#2190)
	}
	body := m.bodyHeight()
	switch m.col {
	case colFrames:
		m.frameTop = clamp(m.frameTop+delta, 0, max(0, len(m.frames)-body))
		if !m.editing {
			m.frameSel = clamp(m.frameSel, m.frameTop, max(m.frameTop, m.frameTop+body-1))
		}
	default:
		m.varTop = clamp(m.varTop+delta, 0, max(0, len(m.flat())-body))
		if !m.editing {
			m.varSel = clamp(m.varSel, m.varTop, max(m.varTop, m.varTop+body-1))
		}
	}
}

// Click handles one left click at content-local (x, y): it focuses the column
// under the cursor, selects the row, and activates it on a double-click (frame
// select / variable expand-collapse) — mirroring enter.
//
// Coordinates outside the interior are rejected (#639): border clicks reach
// this handler because layout.Hit classifies the whole pane rectangle — the
// bottom border arrives as y == m.h (one past the last interior row, which the
// length guard alone would accept on long lists) and the left border/padding
// as x < 0 (which columnAt would map onto the frames column).
func (m *Model) Click(x, y int) tea.Cmd {
	// The internal tab bar leads once a console exists (#2190): a click on
	// its row switches views; body rows sit one lower.
	if bar := m.tabBarRows(); bar > 0 {
		if y == 0 && x >= 0 && x < m.w {
			m.TabBarClick(x)
			m.lastClickAt = time.Time{}
			return nil
		}
		y -= bar
		if m.tab == TabConsole {
			return nil // terminal clicks route app-side, like a terminal pane
		}
	}
	if x < 0 || x >= m.w || y < 0 || y >= m.h {
		m.lastClickAt = time.Time{} // a border click still voids a pending double-click
		return nil
	}
	// A click while the inline value editor is open cancels the edit first
	// (#627): selection then moves normally, so the editor can never render on
	// a different row than it was opened on (nor with a stale buffer).
	if m.editing {
		m.cancelEdit()
	}
	if y == 0 { // the title row has nothing to click; it still resets the tracker
		m.doubleClicked(m.columnAt(x), -1)
		return nil
	}
	switch m.columnAt(x) {
	case colFrames:
		i := m.frameTop + (y - 1)
		double := m.doubleClicked(colFrames, i)
		if i < 0 || i >= len(m.frames) {
			return nil
		}
		m.col = colFrames
		m.frameSel = i
		if double {
			return m.activate()
		}
	default:
		rows := m.flat()
		i := m.varTop + (y - 1)
		double := m.doubleClicked(colVars, i)
		if i < 0 || i >= len(rows) {
			return nil
		}
		m.col = colVars
		m.varSel = i
		if double {
			return m.activate()
		}
	}
	return nil
}

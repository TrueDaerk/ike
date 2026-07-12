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
	hit := m.lastClickTab == m.tab && m.lastClickRow == row &&
		nowAt.Sub(m.lastClickAt) <= doubleClickWindow
	m.lastClickTab, m.lastClickRow, m.lastClickAt = m.tab, row, nowAt
	return hit
}

// Mouse control (#503). Coordinates are pane-content-local: y 0 is the tab
// header, the active view's body starts at y 1.

// header layout: " 1 Changes  │  2 Log" — the clickable label spans.
const (
	hdrChangesFrom, hdrChangesTo = 1, 10
	hdrLogFrom, hdrLogTo         = 15, 20
)

// Wheel scrolls the active view by delta rows (positive = down).
func (m *Model) Wheel(delta int) {
	switch m.tab {
	case TabChanges:
		m.chTop = clampInt(m.chTop+delta, 0, maxInt(0, len(m.chRows)-1))
		m.chCursor = clampInt(m.chCursor, m.chTop, maxInt(m.chTop, m.chTop+m.bodyHeight()-1))
	default:
		m.logTop = clampInt(m.logTop+delta, 0, maxInt(0, len(m.logRows)-1))
		m.logCursor = clampInt(m.logCursor, m.logTop, maxInt(m.logTop, m.logTop+m.bodyHeight()-1))
	}
}

// Click handles one left click at content-local (x, y): tab-header clicks
// switch views, a row click selects, a click on the selected row activates
// it (expand/diff — the changes checkbox region toggles staging instead).
func (m *Model) Click(x, y int) tea.Cmd {
	if y == 0 {
		switch {
		case x >= hdrChangesFrom && x < hdrChangesTo:
			m.tab = TabChanges
		case x >= hdrLogFrom && x < hdrLogTo:
			m.tab = TabLog
			return m.ensureLogLoaded()
		}
		return nil
	}
	if m.tab == TabChanges {
		return m.clickChanges(x, y)
	}
	return m.clickLog(y)
}

// clickChanges maps a body click onto the staging list: rows start at body
// line 0 (y 1). The checkbox region ([x] plus padding) toggles staging.
func (m *Model) clickChanges(x, y int) tea.Cmd {
	i := m.chTop + (y - 1)
	if i < 0 || i >= len(m.chRows) {
		return nil
	}
	m.msgFocus = false
	if x <= 4 {
		// The checkbox region stages/unstages on a single click; it never
		// arms a double-click.
		m.chCursor = i
		m.lastClickRow = -1
		return m.updateChanges(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	}
	// A single click selects; only a double-click opens the diff (#514).
	double := m.doubleClicked(i)
	m.chCursor = i
	if double {
		path := m.chRows[i].Path
		return func() tea.Msg { return OpenDiffMsg{Path: path} }
	}
	return nil
}

// clickLog maps a body click onto the flattened log rows: y 1 is the column
// header, rows start at y 2.
func (m *Model) clickLog(y int) tea.Cmd {
	i := m.logTop + (y - 2)
	if y < 2 || i < 0 || i >= len(m.logRows) {
		return nil
	}
	// A single click selects; only a double-click activates (#514).
	double := m.doubleClicked(i)
	m.logCursor = i
	if double {
		return m.activateLogRow()
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

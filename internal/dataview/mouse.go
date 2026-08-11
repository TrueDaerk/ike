package dataview

// mouse.go is the pane's mouse control (#1788). Before it the data viewer was
// keyboard-only: neither the wheel nor a click reached it, while every other
// pane scrolls and selects with the mouse.
//
// Coordinates are pane-content-local, matching what View draws: y 0 is the
// header line, the body rows are y 1 … bodyHeight, and the footer (plus the
// open filter line) sits below them. Horizontally the sidebar owns
// x < sidebarWidth and the grid everything right of it — the same split
// JoinHorizontal renders.

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// doubleClickWindow is the maximum delay between two clicks on the same
// sidebar row for the second to load the table, matching the explorer and the
// tool panels.
const doubleClickWindow = 400 * time.Millisecond

// Wheel scrolls the focused region by delta rows (positive = down): the table
// list in the sidebar, the loaded page's rows in the grid. The cursor is
// dragged along so it stays inside the visible window. A wheel tick on a grid
// already parked at its page edge fetches the neighbour page, so the wheel
// walks a big table exactly like j/k do.
func (m *Model) Wheel(delta int) {
	if m.err != nil || delta == 0 {
		return
	}
	if m.region == regionSidebar {
		wheelWindow(&m.ttop, &m.tcur, len(m.tables), m.bodyHeight(), delta)
		m.clampScroll()
		return
	}
	if n := len(m.page.Rows); n > 0 {
		top := m.rowTop
		wheelWindow(&m.rowTop, &m.rowCur, n, m.gridHeight(), delta)
		if m.rowTop != top {
			m.clampScroll()
			return
		}
		// The window is pinned at an end of the page; the cursor still walks
		// to that end before the page itself moves.
		if delta > 0 && m.rowCur < n-1 {
			m.rowCur = n - 1
			m.clampScroll()
			return
		}
		if delta < 0 && m.rowCur > 0 {
			m.rowCur = 0
			m.clampScroll()
			return
		}
	}
	// Nothing left to scroll inside the loaded page, so the tick crosses into
	// the neighbour one.
	if delta > 0 && m.hasNextPage() {
		m.fetch(m.page.Offset + PageSize)
		m.rowCur, m.rowTop = 0, 0
	} else if delta < 0 && m.page.Offset > 0 {
		m.fetch(max64(0, m.page.Offset-PageSize))
		m.rowCur = len(m.page.Rows) - 1
	}
	m.clampScroll()
}

// WheelX scrolls the grid's columns by delta (positive = right), the gesture
// the horizontal wheel and shift+wheel carry. It is grid-only: the sidebar has
// nothing to pan.
func (m *Model) WheelX(delta int) {
	if m.err != nil {
		return
	}
	m.colOff += delta
	m.clampScroll()
}

// wheelWindow scrolls a window of height h over n rows by delta and drags the
// cursor back inside it.
func wheelWindow(top, cur *int, n, h, delta int) {
	maxTop := n - h
	if maxTop < 0 {
		maxTop = 0
	}
	*top += delta
	if *top > maxTop {
		*top = maxTop
	}
	if *top < 0 {
		*top = 0
	}
	if *cur < *top {
		*cur = *top
	}
	if *cur >= *top+h {
		*cur = *top + h - 1
	}
	clamp(cur, n)
}

// Click handles one left click at content-local (x, y). The clicked half takes
// the region focus, so the pointer and the keyboard never disagree about where
// the cursor is: a sidebar click selects the object and a second click on it
// within doubleClickWindow loads it (like enter), a grid click moves the row
// cursor.
func (m *Model) Click(x, y int) tea.Cmd {
	// The filter line owns the input while it is open (#1777) — its clause is
	// half-typed text, and loading a table would drop it — so clicks are inert
	// until enter or esc closes the line.
	if m.err != nil || m.fEditing {
		return nil
	}
	if y < 1 || y > m.bodyHeight() {
		return nil
	}
	if x < m.sidebarWidth() {
		m.sidebarClick(m.ttop + y - 1)
		return nil
	}
	m.gridClick(y)
	return nil
}

// sidebarClick selects table i, loading it on the second click.
func (m *Model) sidebarClick(i int) {
	if i < 0 || i >= len(m.tables) {
		return
	}
	double := m.doubleClicked(i)
	m.region, m.tcur = regionSidebar, i
	if double {
		// Unlike enter the region stays in the sidebar: the pointer is there,
		// and double-clicking down the list stays one gesture.
		m.loadTable(i)
		m.region = regionSidebar
	}
	m.clampScroll()
}

// gridClick moves the row cursor to the clicked body row. Row y 1 is the
// column header — clicking it only moves the region focus, there being no row
// under it.
func (m *Model) gridClick(y int) {
	m.region = regionGrid
	m.lastClickRow = -1
	if i := m.rowTop + y - 2; y >= 2 && i >= 0 && i < len(m.page.Rows) {
		m.rowCur = i
	}
	m.clampScroll()
}

// doubleClicked records one click on a sidebar row and reports whether it
// completes a double-click on that row.
func (m *Model) doubleClicked(row int) bool {
	at := time.Now()
	if m.now != nil {
		at = m.now()
	}
	hit := m.lastClickRow == row && at.Sub(m.lastClickAt) <= doubleClickWindow
	m.lastClickRow, m.lastClickAt = row, at
	return hit
}

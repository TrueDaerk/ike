package espane

// mouse.go is the console pane's mouse control (#2259). The pane was
// keyboard-only: neither the wheel nor a click reached it, while the data
// viewer it mirrors in layout and keys has scrolled and selected with the
// mouse since #1788. The gestures follow that pane exactly, with the one
// difference the console is built around — crossing a page edge is a network
// fetch, so the wheel and a double click return the command that runs it
// instead of paging in place.
//
// Coordinates are pane-content-local, matching what View draws: y 0 is the
// header line, the body rows are y 1 … bodyHeight, the status footer sits
// below them. Horizontally the sidebar owns x < sidebarWidth and the grid
// everything right of it — the same split JoinHorizontal renders. Inside the
// grid the body's first row is the column header, so the hit rows start one
// lower.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// headerRows is how many lines sit above the body: the pane's header line.
const headerRows = 1

// gridHeaderRows is what the grid spends on its column header, on top of the
// pane's own header line.
const gridHeaderRows = headerRows + 1

// Wheel scrolls the focused region by delta rows (positive = down): the index
// list in the sidebar, the loaded page's hits in the grid. The cursor is
// dragged along so it stays inside the visible window. A tick on a grid
// already parked at its page edge fetches the neighbour page, so the wheel
// walks a big index exactly like j/k do — asynchronously, one flight at a
// time.
func (m *Model) Wheel(delta int) tea.Cmd {
	if m.err != nil || m.client == nil || delta == 0 {
		return nil
	}
	if m.region == regionSidebar {
		ui.WheelWindow(&m.itop, &m.icur, delta, len(m.indices), m.bodyHeight())
		m.clampScroll()
		return nil
	}
	if m.res == nil {
		return nil
	}
	if n := len(m.res.Rows); n > 0 {
		top := m.rowTop
		ui.WheelWindow(&m.rowTop, &m.rowCur, delta, n, m.gridHeight())
		if m.rowTop != top {
			m.clampScroll()
			return nil
		}
		// The window is pinned at an end of the page; the cursor still walks
		// to that end before the page itself moves.
		if delta > 0 && m.rowCur < n-1 {
			m.rowCur = n - 1
			m.clampScroll()
			return nil
		}
		if delta < 0 && m.rowCur > 0 {
			m.rowCur = 0
			m.clampScroll()
			return nil
		}
	}
	// Nothing left to scroll inside the loaded page, so the tick crosses into
	// the neighbour one — the same fetch j/k trigger at the page edge.
	if delta > 0 && m.hasNextPage() {
		return m.fetchCmd(m.res.From + PageSize)
	}
	if delta < 0 && m.res.From > 0 {
		cmd := m.fetchCmd(maxInt(0, m.res.From-PageSize))
		m.pendingBottom = cmd != nil
		return cmd
	}
	return nil
}

// WheelX pans the grid's columns by delta (positive = right), the gesture the
// horizontal wheel and shift+wheel carry. It is grid-only: the sidebar has
// nothing to pan.
func (m *Model) WheelX(delta int) {
	if m.err != nil || m.client == nil {
		return
	}
	m.colOff += delta
	m.clampScroll()
}

// Click handles one left click at content-local (x, y). The clicked half
// takes the region focus, so the pointer and the keyboard never disagree
// about where the cursor is: a sidebar click selects the index and a second
// click on it within the double-click window loads it (like enter), a grid
// click moves the row cursor.
func (m *Model) Click(x, y int) tea.Cmd {
	if m.err != nil || m.client == nil {
		return nil
	}
	if y < headerRows || y >= headerRows+m.bodyHeight() {
		return nil
	}
	if x < m.sidebarWidth() {
		return m.sidebarClick(y)
	}
	m.gridClick(y)
	return nil
}

// sidebarClick selects the index under content-local row y, loading it on the
// second click.
func (m *Model) sidebarClick(y int) tea.Cmd {
	i, ok := ui.RowAt(y, m.itop, headerRows, m.bodyHeight(), len(m.indices))
	if !ok {
		m.clicks.Reset()
		return nil
	}
	double := m.clicks.Double(i, m.clock())
	m.region, m.icur = regionSidebar, i
	m.clampScroll()
	if !double {
		return nil
	}
	// Clear the pending click after the load, so a third click does not load
	// again. Unlike enter the region stays in the sidebar: the pointer is
	// there, and double-clicking down the list stays one gesture.
	m.clicks.Reset()
	if i == m.sel {
		return nil // already loaded; a re-run is the r key's job
	}
	cmd := m.loadIndex(i)
	m.region = regionSidebar
	return cmd
}

// gridClick moves the row cursor to the clicked hit row. The body's first row
// is the column header — clicking it only moves the region focus, there being
// no hit under it.
func (m *Model) gridClick(y int) {
	m.region = regionGrid
	m.clicks.Reset()
	rows := 0
	if m.res != nil {
		rows = len(m.res.Rows)
	}
	if i, ok := ui.RowAt(y, m.rowTop, gridHeaderRows, m.gridHeight(), rows); ok {
		m.rowCur = i
	}
	m.clampScroll()
}

// clock reads the injectable clock; a pane built without one falls back to
// the wall clock.
func (m *Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

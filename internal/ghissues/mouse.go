package ghissues

// mouse.go: wheel scroll plus click-select / double-click-open, mirroring the
// Usages panel (#514) — activating a row needs a second click on the same row
// within doubleClickWindow — extended in #2090 with the tab bar as a click
// target and with hit-testing that accounts for the filter row and the
// detail view's position header.

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// doubleClickWindow bounds the second click of a double-click.
const doubleClickWindow = 400 * time.Millisecond

// Wheel scrolls the active view (or the open modal / detail) by delta rows.
func (m *Model) Wheel(delta int) {
	switch {
	case m.ov != ovNone:
		m.ovCursor += delta
		m.clampOverlay()
	case m.detail && m.tab == TabIssues:
		m.detailTop += delta
		m.clampDetail()
	default:
		rows := m.rowsOf(m.tab)
		m.setCursor(snapRow(rows, m.Cursor()+delta, sign(delta)))
		m.clampScroll()
	}
}

// sign is the step direction a wheel delta implies, used to skip group
// headers the way a key press does.
func sign(delta int) int {
	if delta < 0 {
		return -1
	}
	return 1
}

// Click handles one left click at pane-local (x, y): the tab bar switches the
// view, a body click selects the row, a second click on the same row within
// the window opens its detail (the PR view has no detail yet — #2089 — so a
// double-click there opens the pull request in the browser).
func (m *Model) Click(x, y int) tea.Cmd {
	if y == 0 {
		m.clickTabBar(x)
		return nil
	}
	if m.ov != ovNone {
		return m.clickOverlay(y)
	}
	if m.fEditing && y == 1 {
		return nil // the open filter line is not a row
	}
	if m.detail && m.tab == TabIssues {
		return nil
	}
	rows := m.rowsOf(m.tab)
	row := m.Top() + y - m.bodyTop()
	if y < m.bodyTop() || row < 0 || row >= len(rows) || rows[row].idx < 0 {
		return nil
	}
	m.setCursor(row)
	m.clampScroll()
	if row == m.lastClickRow && m.clock().Sub(m.lastClickAt) <= doubleClickWindow {
		m.lastClickRow, m.lastClickAt = -1, time.Time{}
		return m.activate()
	}
	m.lastClickRow, m.lastClickAt = row, m.clock()
	return nil
}

// clickTabBar resolves a click on the pane's first row to the view whose
// label it landed on.
func (m *Model) clickTabBar(x int) {
	for i, span := range m.tabBarSpans() {
		if x >= span[0] && x < span[1] {
			m.SetTab(Tab(i))
			return
		}
	}
}

// clickOverlay moves the open modal's cursor to the clicked row; the modal
// box is centered in the body, so only its own rows are hit-tested.
func (m *Model) clickOverlay(y int) tea.Cmd {
	h := m.overlayHeight()
	// The box is centered over the body: one border row plus the heading sit
	// above its first entry.
	first := m.bodyTop() + (m.bodyHeight()-(h+3))/2 + 2
	row := m.ovTop + y - first
	if row < 0 || row >= m.overlayItems() {
		return nil
	}
	m.ovCursor = row
	m.clampOverlay()
	return nil
}

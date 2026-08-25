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
// It returns the timeline page a scroll to the end of the issue detail pulls
// in (#2113), nil otherwise.
func (m *Model) Wheel(delta int) tea.Cmd {
	switch {
	case m.ov != ovNone:
		m.ovCursor += delta
		m.clampOverlay()
	case m.detail && m.tab == TabIssues:
		m.detailTop += delta
		m.clampDetail()
		if delta > 0 {
			return m.autoLoadTimeline()
		}
	case m.prDetail && m.tab == TabPRs:
		m.prdTop += delta
		m.clampPRDetail()
	default:
		rows := m.rowsOf(m.tab)
		m.setCursor(snapRow(rows, m.Cursor()+delta, sign(delta)))
		m.clampScroll()
	}
	return nil
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
// the window opens its detail — the issue detail or, on the PR tab, the PR
// detail (#2089).
func (m *Model) Click(x, y int) tea.Cmd {
	if y == 0 {
		m.clickTabBar(x)
		return nil
	}
	if m.ov != ovNone {
		return m.clickOverlay(y)
	}
	if y == 1 {
		// The permanent filter row (#2104): a click on a chip clears exactly
		// that narrowing.
		return m.clearChip(x)
	}
	if m.detailShown() {
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
	// Clicking one of the filter overlay's fixed rows leaves the label
	// section, and with it the section's type-ahead (#2111) — otherwise the
	// next keypress would snap the cursor back into the narrowed labels.
	if m.ov == ovFilter && row < m.fovFixedRows() {
		m.ovSearch.Reset()
	}
	m.ovCursor = row
	m.clampOverlay()
	return nil
}

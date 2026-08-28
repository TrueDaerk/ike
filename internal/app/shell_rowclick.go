package app

import (
	tea "charm.land/bubbletea/v2"
)

// shell_rowclick.go is the pointer half of the floating shell's hosted
// pickers (#2275). #2259 gave the shell a wheel but left one gap open: the
// shell renders its content through Content.Render into a scroller, so it sees
// opaque text and could not map a clicked line back onto an item. Clicking a
// row therefore did nothing — the keyboard was the only way to drive a picker.
//
// The seam is ui.Floating.BodyPoint: the shell owns the coordinate math (its
// chrome origin plus the rows scrolled off the top) and hands the host
// body-local content coordinates. The root model's mouse handler calls it once
// and passes the result here; every picker below maps that row onto one of its
// items, the inverse of its render loop.
//
// The convention is #2259's, in the overlay variant the finder and the
// settings panel already use: a click selects the row, and a click on the
// already-selected row activates it — the same thing enter does there. A
// picker with no enter action (crash recovery) selects only, and rows that are
// not items — headings, group titles, separators, the blank tail, the key
// legends — are inert.

// shellBodyClick routes a left press on the shell body to the picker that
// currently owns the shell. x and y are body-local content coordinates, so no
// picker repeats the chrome-origin arithmetic. It reports whether a picker
// took the press; false lets the caller fall through to its own routing.
func (m Model) shellBodyClick(msg mouseEvent, x, y int) (tea.Model, tea.Cmd, bool) {
	switch {
	case m.generateScratchOpen():
		// The test-data wizard (#2228): the click acts on the hit region the
		// last render recorded for that body line.
		out, cmd, _ := m.mouseGenerateScratch(msg, x, y)
		return out, cmd, true
	case m.scratchManagerOpen():
		// The scratch manager (#2256) hit-tests its rows and buttons itself.
		out, cmd, _ := m.mouseScratchManager(msg, x, y)
		return out, cmd, true
	case m.layoutSelectOpen():
		// The save-layout mini-map (#1570) is the one two-dimensional body:
		// a click on a cell focuses and toggles that pane.
		m.layoutSelectClick(x, y)
		return m, nil, true
	}
	// Row-shaped pickers: the line under the pointer is the whole hit-test.
	// The order mirrors the key routing in Update — two pickers whose state
	// happens to be live at once must answer the pointer and the keyboard the
	// same way.
	var f func(int) (tea.Model, tea.Cmd)
	switch {
	case m.recoveryOpen():
		f = m.recoveryClickRow
	case m.onboardingOpen():
		f = m.onboardingClickRow
	case m.themePickOpen():
		f = m.themePickClickRow
	case m.toolSetupOpen():
		f = m.toolSetupClickRow
	case m.pinPickerOpen():
		f = m.pinPickerClickRow
	case m.localHistoryOpen():
		f = m.localHistoryClickRow
	case m.timelineOpen():
		f = m.timelineClickRow
	case m.changeFeedOpen():
		f = m.changeFeedClickRow
	case m.historyPickerOpen():
		f = m.historyPickerClickRow
	case m.bookmarkOverviewOpen():
		f = m.bookmarkOverviewClickRow
	case m.lspRenamePreviewOpen():
		f = m.lspRenamePreviewClickRow
	default:
		return m, nil, false
	}
	out, cmd := f(y)
	return out, cmd, true
}

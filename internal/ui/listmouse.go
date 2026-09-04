package ui

// listmouse.go is the shared mouse layer for list-shaped surfaces (#2259).
// It is to the pointer what listnav.go is to the keyboard: before it every
// list pane rolled its own wheel arithmetic, its own row hit-test and its own
// double-click clock, so a wheel flick scrolled the last page off the screen
// in nine panes but not in two, a click on the footer line selected a row in
// three of them, and two panes had no mouse support at all.
//
// The convention these primitives fix, for every list-shaped surface:
//
//   - the wheel scrolls the list and never past its last full page; the
//     selection is dragged along so it stays inside the visible window,
//   - a single click selects the row under the pointer (the pane focus is the
//     app's job, before the event reaches the pane),
//   - a second click on the same row within DoubleClickWindow activates it —
//     exactly what enter does.
//
// Coordinates are pane-content-local throughout, matching what a pane's View
// draws: y 0 is its first rendered line, so a pane whose body starts below a
// header passes that line count as headerRows.

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// DoubleClickWindow is the maximum delay between two clicks on the same row
// for the second to activate it. One value for every surface — the explorer,
// the tool panels, the viewers — so the gesture feels the same everywhere.
const DoubleClickWindow = 400 * time.Millisecond

// WheelWindow scrolls a height-row window over n rows by delta rows (positive
// = down) and drags the cursor back inside it. The scroll clamps at both ends:
// the last page never scrolls past its final row, which is the invariant a
// keyboard-driven clampScroll maintains and what the wheel must match. Both
// pointers are left valid for an empty list.
func WheelWindow(top, cursor *int, delta, n, height int) {
	if height < 1 {
		height = 1
	}
	if n <= 0 {
		*top, *cursor = 0, 0
		return
	}
	maxTop := n - height
	if maxTop < 0 {
		maxTop = 0
	}
	*top = clampTo(*top+delta, 0, maxTop)
	*cursor = ClampIndex(clampTo(*cursor, *top, *top+height-1), n)
}

// RowAt maps a content-local row y onto a list index, given the window's
// current top, how many header lines sit above the body (headerRows), the body
// height in rows and the list length. It reports ok=false for the header rows,
// for anything at or below the body — the footer, the key hints — and for the
// blank rows a short list leaves at the bottom of its window.
func RowAt(y, top, headerRows, height, n int) (int, bool) {
	if height < 0 {
		height = 0
	}
	if y < headerRows || y >= headerRows+height {
		return 0, false
	}
	i := top + (y - headerRows)
	if i < 0 || i >= n {
		return 0, false
	}
	return i, true
}

// ClickTracker records the last click a surface saw so the next one can be
// recognised as a double click on the same row. Its zero value is an empty
// tracker: the first click on any row is always single.
//
// Row identity is the caller's to define — a list index, or a synthetic id
// where one surface hosts several lists (the explorer offsets its scratch
// rows past the tree rows so a tree click and a section click never pair up).
type ClickTracker struct {
	row int
	at  time.Time
	set bool
}

// Double records one click on row at time now and reports whether it completes
// a double click on that row.
func (t *ClickTracker) Double(row int, now time.Time) bool {
	hit := t.set && t.row == row && now.Sub(t.at) <= DoubleClickWindow
	t.row, t.at, t.set = row, now, true
	return hit
}

// Reset clears the pending click, so the next one counts as a first click
// again. Surfaces call it after activating a row — otherwise a third click
// would activate a second time — and wherever a click lands somewhere that is
// not a row at all.
func (t *ClickTracker) Reset() { *t = ClickTracker{} }

// ClickRow is the whole left-click gesture of a list pane in one call
// (#2462): it hit-tests content-local row y, moves cursor onto the row it
// found and, when the same row was already clicked inside DoubleClickWindow,
// activates it and returns the command that does so. A click that is not on a
// row clears the pending click, so the next one counts as a first click.
//
// activate may be nil for a list whose rows have no enter action; such a pane
// only ever selects.
func (t *ClickTracker) ClickRow(y, top, headerRows, height, n int, now time.Time, cursor *int, activate func(int) tea.Cmd) tea.Cmd {
	i, ok := RowAt(y, top, headerRows, height, n)
	if !ok {
		t.Reset()
		return nil
	}
	double := t.Double(i, now)
	*cursor = i
	if double && activate != nil {
		t.Reset()
		return activate(i)
	}
	return nil
}

// SelectClick moves cursor onto the row under content-local y and reports
// whether it hit one. It is ClickRow without the double-click clock, for the
// read-only report panes whose rows have no action behind them.
func SelectClick(y, top, headerRows, height, n int, cursor *int) bool {
	i, ok := RowAt(y, top, headerRows, height, n)
	if ok {
		*cursor = i
	}
	return ok
}

// clampTo confines v to [lo, hi]; hi below lo collapses onto lo.
func clampTo(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

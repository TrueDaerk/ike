package dataview

// sort.go is the grid's column sort (#2248). `S` on the grid — or
// `data.sortColumn` from the palette — cycles the focused column through
// **ascending, descending, none**, and every step refetches from the top of
// the result.
//
// Three decisions make it small:
//
//   - **The pane still holds no SQL.** The sort travels as a datasrc.Sort to
//     PageWhere, and the backend renders the ORDER BY outside the filter's
//     subquery. SQLite, DuckDB and Parquet therefore sort identically, and a
//     column name never becomes a string the pane concatenates into a query.
//   - **The focused column is the one the profile uses** — the leftmost
//     visible one, which `h`/`l` move. The grid scrolls columns instead of
//     carrying a column cursor, so a second cursor just for sorting would be
//     a second thing to keep in sync.
//   - **A sort restarts paging.** A different order makes offset 500 a
//     different set of rows, so the cycle jumps back to the first page rather
//     than leaving the cursor on a row that has moved.
//
// The row *count* is untouched by ordering, so the count cache (#1795) keys on
// (table, filter) as before: sorting a counted table issues no new COUNT(*).

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/datasrc"
)

// SortMsg asks a focused data pane to cycle the sort of its focused column —
// the palette's `data.sortColumn`, the same action the pane's `S` runs.
type SortMsg struct{}

// Sort returns the applied column sort (tests).
func (m *Model) Sort() datasrc.Sort { return m.sort }

// toggleSort cycles the focused column: unsorted → ascending → descending →
// unsorted. Sorting a different column starts that column at ascending, which
// is what "sort by this" means when the previous sort was elsewhere.
func (m *Model) toggleSort() {
	column, ok := m.focusedColumn()
	if !ok || m.src == nil {
		return
	}
	switch {
	case m.sort.Column != column:
		m.sort = datasrc.Sort{Column: column}
	case !m.sort.Desc:
		m.sort.Desc = true
	default:
		m.sort = datasrc.Sort{}
	}
	// The order decides which rows a page holds, so the walk starts over.
	m.rowCur, m.rowTop = 0, 0
	m.fetch(0)
}

// clearSort drops the sort, which switching tables does: a column of the old
// table cannot order the new one.
func (m *Model) clearSort() { m.sort = datasrc.Sort{} }

// sortMarker is the arrow the header row appends to the sorted column, "" for
// every other column — the sort is a property of the grid, so it is shown on
// the grid rather than only in the header line.
func (m *Model) sortMarker(column string) string {
	if !m.sort.Active() || m.sort.Column != column {
		return ""
	}
	if m.sort.Desc {
		return " ▼"
	}
	return " ▲"
}

// sortColumnKey runs the sort key and returns nothing to the runtime: the
// fetch is synchronous like every other page fetch, and only the count
// command the caller batches may follow.
func (m *Model) sortColumnKey() tea.Cmd {
	m.toggleSort()
	m.clampScroll()
	return nil
}

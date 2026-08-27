package dataview

// sort_test.go covers the pane's half of the column sort (#2248): S cycles
// the focused column, the order reaches the backend and survives paging, the
// header marks it, and switching tables drops it.

import (
	"strings"
	"testing"
)

// sqlGridPane is a focused pane over a real SQLite fixture with `users`
// loaded and the grid region active — the sort is the backend's ORDER BY, so
// it wants a real engine rather than a fake.
func sqlGridPane(t *testing.T, path string) Model {
	t.Helper()
	m := newPane(t, path)
	pump(t, &m, m.Update(key("j")))     // `empty` → `users`
	pump(t, &m, m.Update(key("enter"))) // load it, and move into the grid
	if !m.InGrid() || m.SelectedTable() != "users" {
		t.Fatalf("the fixture's users table must be loaded in the grid, got %q", m.SelectedTable())
	}
	return m
}

func TestSortCyclesTheFocusedColumn(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	if m.Sort().Active() {
		t.Fatal("a fresh grid is unsorted")
	}
	pump(t, &m, m.Update(key("S")))
	if s := m.Sort(); s.Column != "id" || s.Desc {
		t.Fatalf("the first S sorts the focused column ascending, got %+v", s)
	}
	if got := m.page.Rows[0][0].Text; got != "1" {
		t.Fatalf("ascending first row = %q, want 1", got)
	}
	pump(t, &m, m.Update(key("S")))
	if s := m.Sort(); s.Column != "id" || !s.Desc {
		t.Fatalf("the second S sorts descending, got %+v", s)
	}
	if got := m.page.Rows[0][0].Text; got != "1200" {
		t.Fatalf("descending first row = %q, want 1200", got)
	}
	pump(t, &m, m.Update(key("S")))
	if m.Sort().Active() {
		t.Fatal("the third S drops the sort")
	}
	if got := m.page.Rows[0][0].Text; got != "1" {
		t.Fatalf("the unsorted grid comes back, got first row %q", got)
	}
}

func TestSortMovesToAnotherColumnAscending(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	pump(t, &m, m.Update(key("S")))
	pump(t, &m, m.Update(key("S"))) // id descending
	pump(t, &m, m.Update(key("l"))) // focus `name`
	pump(t, &m, m.Update(key("S")))
	if s := m.Sort(); s.Column != "name" || s.Desc {
		t.Fatalf("sorting another column starts ascending, got %+v", s)
	}
}

func TestSortPagesAndRestartsAtTheTop(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	pump(t, &m, m.Update(key("n"))) // page 2
	if m.PageOffset() != PageSize {
		t.Fatalf("offset = %d, want %d", m.PageOffset(), PageSize)
	}
	pump(t, &m, m.Update(key("S")))
	pump(t, &m, m.Update(key("S"))) // descending
	if m.PageOffset() != 0 || m.Cursor() != 0 {
		t.Fatalf("a new order restarts the walk, got offset %d cursor %d", m.PageOffset(), m.Cursor())
	}
	pump(t, &m, m.Update(key("n")))
	if m.PageOffset() != PageSize {
		t.Fatalf("offset = %d, want %d", m.PageOffset(), PageSize)
	}
	// Page two of a descending walk continues where page one stopped.
	if got := m.page.Rows[0][0].Text; got != "700" {
		t.Fatalf("descending page 2 starts at %q, want 700", got)
	}
}

func TestSortIsMarkedInTheHeaderAndTheGrid(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	pump(t, &m, m.Update(key("S")))
	view := m.View()
	if !strings.Contains(view, "id ▲") {
		t.Fatalf("the sorted column must carry its arrow:\n%s", view)
	}
	if !strings.Contains(view, "sort: id ▲") {
		t.Fatalf("the header must state the sort — the column may be scrolled away:\n%s", view)
	}
	pump(t, &m, m.Update(key("S")))
	if view = m.View(); !strings.Contains(view, "id ▼") {
		t.Fatalf("a descending sort shows the other arrow:\n%s", view)
	}
}

func TestSortDropsWithTheTable(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	pump(t, &m, m.Update(key("S")))
	pump(t, &m, m.Update(key("h"))) // back to the sidebar at the left edge
	pump(t, &m, m.Update(key("j")))
	pump(t, &m, m.Update(key("enter")))
	if m.Sort().Active() {
		t.Fatal("a column of the old table cannot order the new one")
	}
}

func TestSortComposesWithTheFilter(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	pump(t, &m, m.Update(key("S")))
	pump(t, &m, m.Update(key("S"))) // id descending
	pump(t, &m, m.Update(key("/")))
	typeInto(t, &m, "id <= 4")
	pump(t, &m, m.Update(key("enter")))
	if m.Filter() != "WHERE id <= 4" {
		t.Fatalf("filter = %q", m.Filter())
	}
	if m.PageRows() != 4 {
		t.Fatalf("the filter must narrow the grid, got %d rows", m.PageRows())
	}
	if got := m.page.Rows[0][0].Text; got != "4" {
		t.Fatalf("the sort must survive the filter, first row = %q", got)
	}
}

func TestSortMsgIsTheSameActionAsTheKey(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	pump(t, &m, m.Update(SortMsg{}))
	if s := m.Sort(); s.Column != "id" || s.Desc {
		t.Fatalf("data.sortColumn must cycle like S, got %+v", s)
	}
}

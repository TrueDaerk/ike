package dataview

import (
	"testing"
	"time"
)

// clickPane clicks and settles the background work the click started.
func clickPane(t *testing.T, m *Model, x, y int) {
	t.Helper()
	pump(t, m, m.Click(x, y))
}

// clickAt drives one click through a fixed clock so the double-click window is
// deterministic; step is how far the clock advances before the click. The
// click's command is settled like a key's (#1795): a double click loads a
// table, whose row count runs in the background.
func clickAt(t *testing.T, m *Model, now *time.Time, step time.Duration, x, y int) {
	t.Helper()
	*now = now.Add(step)
	m.now = func() time.Time { return *now }
	clickPane(t, m, x, y)
}

// TestWheelScrollsBothRegions: the wheel scrolls whichever half has the region
// focus, dragging its cursor along.
func TestWheelScrollsBothRegions(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter")) // users loaded, region = grid
	m.Wheel(5)
	if m.rowTop != 5 || m.Cursor() != 5 {
		t.Fatalf("grid wheel: top=%d cursor=%d", m.rowTop, m.Cursor())
	}
	// Wheeling back only moves the window; the cursor is dragged along solely
	// when it would leave it.
	m.Wheel(-2)
	if m.rowTop != 3 || m.Cursor() != 5 {
		t.Fatalf("grid wheel back: top=%d cursor=%d", m.rowTop, m.Cursor())
	}
	// The sidebar is shorter than the body, so its window cannot scroll — the
	// wheel must not move the grid instead.
	feed(t, &m, key("tab"))
	top := m.rowTop
	m.Wheel(3)
	if m.rowTop != top {
		t.Fatalf("a sidebar wheel moved the grid: top=%d", m.rowTop)
	}
	if m.InGrid() {
		t.Fatal("the wheel must not change the region")
	}
}

// TestWheelCrossesPageEdges: a wheel tick on a grid already parked at the
// page's edge fetches the neighbour page, like j/k do.
func TestWheelCrossesPageEdges(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	for i := 0; i < 200 && m.PageOffset() == 0; i++ {
		m.Wheel(10)
	}
	if m.PageOffset() != PageSize || m.Cursor() != 0 {
		t.Fatalf("wheel at the page bottom must fetch the next page: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
	for i := 0; i < 200 && m.PageOffset() > 0; i++ {
		m.Wheel(-10)
	}
	if m.PageOffset() != 0 || m.Cursor() != PageSize-1 {
		t.Fatalf("wheel at the page top must fetch the previous page: offset=%d cursor=%d", m.PageOffset(), m.Cursor())
	}
}

// TestWheelXScrollsColumns: the horizontal wheel pans the grid's columns and
// clamps at both ends.
func TestWheelXScrollsColumns(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	m.WheelX(1)
	if m.colOff != 1 {
		t.Fatalf("colOff = %d after a right wheel", m.colOff)
	}
	m.WheelX(20)
	if m.colOff != len(m.page.Columns)-1 {
		t.Fatalf("colOff = %d, want the last column", m.colOff)
	}
	m.WheelX(-20)
	if m.colOff != 0 {
		t.Fatalf("colOff = %d after wheeling left past the start", m.colOff)
	}
}

// TestSidebarClickSelectsAndDoubleClickLoads: one click moves the sidebar
// cursor and takes the region, a second one within the double-click window
// loads the table like enter.
func TestSidebarClickSelectsAndDoubleClickLoads(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("tab")) // region = grid
	now := time.Now()
	// Row y=1 is the first object, y=2 the second ("users" in the fixture).
	clickAt(t, &m, &now, 0, 3, 2)
	if m.InGrid() {
		t.Fatal("a sidebar click must move the region focus to the sidebar")
	}
	if m.tcur != 1 {
		t.Fatalf("sidebar cursor = %d", m.tcur)
	}
	if m.SelectedTable() != "empty" {
		t.Fatalf("a single click must not load: selected = %q", m.SelectedTable())
	}
	clickAt(t, &m, &now, 50*time.Millisecond, 3, 2)
	if m.SelectedTable() != "users" || m.PageRows() != PageSize {
		t.Fatalf("double click: selected=%q rows=%d", m.SelectedTable(), m.PageRows())
	}
	if m.InGrid() {
		t.Fatal("a double click stays in the sidebar the pointer is on")
	}
	// Two clicks far apart are two single clicks, not a load.
	feed(t, &m, key("g")) // back to the first page of users
	clickAt(t, &m, &now, 0, 3, 1)
	clickAt(t, &m, &now, 2*doubleClickWindow, 3, 1)
	if m.SelectedTable() != "users" {
		t.Fatalf("clicks outside the window must not load: selected = %q", m.SelectedTable())
	}
}

// TestGridClickMovesRowCursor: a click on a data row selects it and takes the
// region; the column header row only moves the focus, and a click past the
// loaded rows leaves the cursor alone.
func TestGridClickMovesRowCursor(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	feed(t, &m, key("tab")) // region = sidebar
	x := m.sidebarWidth() + 4
	clickPane(t, &m, x, 5) // body row 5 = column header (y 1) + 3 data rows
	if !m.InGrid() {
		t.Fatal("a grid click must move the region focus to the grid")
	}
	if m.Cursor() != 3 {
		t.Fatalf("row cursor = %d, want 3", m.Cursor())
	}
	clickPane(t, &m, x, 1) // the column header row
	if m.Cursor() != 3 {
		t.Fatalf("a header-row click moved the cursor to %d", m.Cursor())
	}
	// Below the last body row (the footer) nothing is hit.
	clickPane(t, &m, x, m.bodyHeight()+1)
	if m.Cursor() != 3 {
		t.Fatalf("a footer click moved the cursor to %d", m.Cursor())
	}
	// An empty table has no row under the pointer.
	feed(t, &m, key("tab"))
	clickPane(t, &m, 3, 1) // "empty" is the first object
	feed(t, &m, key("enter"))
	clickPane(t, &m, x, 3)
	if m.Cursor() != 0 {
		t.Fatalf("a click into an empty grid set the cursor to %d", m.Cursor())
	}
}

// TestClickIsInertWhileFiltering: the filter line owns the input (#1777), so a
// stray click neither loads another table under the half-typed clause nor
// closes the line.
func TestClickIsInertWhileFiltering(t *testing.T) {
	m := newPane(t, writeFixtureDB(t))
	feed(t, &m, key("j"))
	feed(t, &m, key("enter"))
	feed(t, &m, key("/"))
	feed(t, &m, key("i"))
	feed(t, &m, key("d"))
	if !m.Filtering() || m.FilterInput() != "id" {
		t.Fatalf("filter line: open=%v input=%q", m.Filtering(), m.FilterInput())
	}
	now := time.Now()
	clickAt(t, &m, &now, 0, 3, 1)
	clickAt(t, &m, &now, 50*time.Millisecond, 3, 1)
	clickPane(t, &m, m.sidebarWidth()+4, 4)
	if !m.Filtering() || m.FilterInput() != "id" {
		t.Fatalf("a click broke the open filter line: open=%v input=%q", m.Filtering(), m.FilterInput())
	}
	if m.SelectedTable() != "users" {
		t.Fatalf("a click loaded %q while the filter line was open", m.SelectedTable())
	}
	// Typing still lands in the clause after the clicks.
	feed(t, &m, key("x"))
	if m.FilterInput() != "idx" {
		t.Fatalf("filter input = %q", m.FilterInput())
	}
}

package espane

import (
	"testing"
	"time"

	"ike/internal/ui"
)

// clockPane is an opened console pane whose double-click clock the test
// drives.
func clockPane(t *testing.T, f *fakeCluster) (*Model, *time.Time) {
	t.Helper()
	m := newPane(t, f)
	now := time.Unix(0, 0)
	m.now = func() time.Time { return now }
	return m, &now
}

// TestSidebarClickSelectsIndex guards the single click: it moves the sidebar
// cursor and the region focus without loading anything.
func TestSidebarClickSelectsIndex(t *testing.T) {
	f := newFakeCluster(t, 10)
	m, _ := clockPane(t, f)
	searches := f.searches
	// Sidebar rows start at y 1: empty, logs, all-logs (aliases last).
	if cmd := m.Click(2, 2); cmd != nil {
		t.Fatal("a single click must not load the index")
	}
	if m.CursorIndex() != "logs" {
		t.Fatalf("sidebar cursor on %q, want logs", m.CursorIndex())
	}
	if m.InGrid() {
		t.Fatal("a sidebar click must leave the region in the sidebar")
	}
	if f.searches != searches {
		t.Fatalf("searches = %d, want %d — no fetch on a single click", f.searches, searches)
	}
}

// TestSidebarDoubleClickLoadsIndex guards the activation gesture: the second
// click runs the same load enter runs, and the region stays under the pointer.
func TestSidebarDoubleClickLoadsIndex(t *testing.T) {
	f := newFakeCluster(t, 10)
	m, now := clockPane(t, f)
	m.Click(2, 2)
	*now = now.Add(100 * time.Millisecond)
	pump(t, m, m.Click(2, 2))
	if m.SelectedIndex() != "logs" {
		t.Fatalf("loaded index = %q, want logs", m.SelectedIndex())
	}
	if m.InGrid() {
		t.Fatal("a double click must keep the region in the sidebar")
	}
}

// TestSlowSecondSidebarClickOnlySelects guards the window.
func TestSlowSecondSidebarClickOnlySelects(t *testing.T) {
	f := newFakeCluster(t, 10)
	m, now := clockPane(t, f)
	m.Click(2, 2)
	*now = now.Add(ui.DoubleClickWindow + time.Millisecond)
	if cmd := m.Click(2, 2); cmd != nil {
		t.Fatal("a slow second click must not load the index")
	}
}

// TestGridClickMovesRowCursor guards the grid half: a click takes the region
// focus and puts the row cursor on the clicked hit; the column-header row
// only focuses.
func TestGridClickMovesRowCursor(t *testing.T) {
	f := newFakeCluster(t, 10)
	m, _ := clockPane(t, f)
	pump(t, m, m.loadIndex(1)) // "logs", the index with hits
	x := m.SidebarWidth() + 4
	// Body y 1 is the column header, so y 4 is the third hit.
	m.Click(x, 4)
	if !m.InGrid() {
		t.Fatal("a grid click must take the region focus")
	}
	if m.rowCur != 2 {
		t.Fatalf("row cursor = %d, want 2", m.rowCur)
	}
	m.Click(x, 1)
	if m.rowCur != 2 {
		t.Fatalf("a column-header click moved the row cursor to %d", m.rowCur)
	}
}

// TestWheelScrollsSidebarAndGrid guards the wheel in both halves: the sidebar
// list scrolls with its cursor dragged along, the grid walks its loaded page.
func TestWheelScrollsSidebarAndGrid(t *testing.T) {
	f := newFakeCluster(t, 300)
	m, _ := clockPane(t, f)
	m.SetSize(100, 8) // body height 6, grid height 5
	if cmd := m.Wheel(2); cmd != nil {
		t.Fatal("a sidebar wheel notch must not fetch")
	}
	// Three indices in a six-row window: there is nothing to scroll.
	if m.itop != 0 {
		t.Fatalf("sidebar top = %d, want 0 — the list is shorter than the window", m.itop)
	}
	pump(t, m, m.loadIndex(1))     // "logs", the index with hits
	m.Click(m.SidebarWidth()+4, 2) // move the region into the grid
	m.Wheel(3)
	if m.rowTop != 3 {
		t.Fatalf("grid top = %d, want 3", m.rowTop)
	}
	if m.rowCur < m.rowTop || m.rowCur >= m.rowTop+m.gridHeight() {
		t.Fatalf("row cursor %d left the window", m.rowCur)
	}
}

// TestWheelAtPageEdgeFetchesNextPage guards the page crossing: once the
// window and the cursor sit at the end of the loaded page, the next notch
// returns the fetch command the keyboard's page walk returns.
func TestWheelAtPageEdgeFetchesNextPage(t *testing.T) {
	f := newFakeCluster(t, 300)
	m, _ := clockPane(t, f)
	pump(t, m, m.loadIndex(1)) // "logs", the index with hits
	m.Click(m.SidebarWidth()+4, 2)
	from := m.PageFrom()
	pump(t, m, m.Wheel(PageSize*2)) // park the window at the bottom of page one
	pump(t, m, m.Wheel(1))          // walks the cursor onto the page's last hit
	if m.PageFrom() != from {
		t.Fatalf("page from = %d, want %d — the cursor walks to the edge first", m.PageFrom(), from)
	}
	pump(t, m, m.Wheel(1)) // the cursor is at the edge: this one crosses
	if m.PageFrom() != from+PageSize {
		t.Fatalf("page from = %d, want %d — the edge notch must fetch on", m.PageFrom(), from+PageSize)
	}
	// And back: the landed page put the cursor at its top, so the very next
	// upward notch crosses to the previous page.
	pump(t, m, m.Wheel(-1))
	if m.PageFrom() != from {
		t.Fatalf("page from = %d, want back to %d", m.PageFrom(), from)
	}
}

// TestWheelXPansColumns guards the horizontal gesture: it pans the grid's
// columns, which is what shift+wheel and a horizontal wheel carry.
func TestWheelXPansColumns(t *testing.T) {
	f := newFakeCluster(t, 10)
	m, _ := clockPane(t, f)
	pump(t, m, m.loadIndex(1)) // "logs", the index with hits
	m.WheelX(1)
	if m.colOff != 1 {
		t.Fatalf("colOff = %d, want 1", m.colOff)
	}
	m.WheelX(-10)
	if m.colOff != 0 {
		t.Fatalf("colOff = %d, want clamped to 0", m.colOff)
	}
}

// TestClickOnChromeIsInert guards the hit-test: the header line and anything
// below the body select nothing.
func TestClickOnChromeIsInert(t *testing.T) {
	f := newFakeCluster(t, 10)
	m, _ := clockPane(t, f)
	before := m.CursorIndex()
	for _, y := range []int{0, 23, 40} {
		if cmd := m.Click(2, y); cmd != nil {
			t.Fatalf("click at y %d must be inert", y)
		}
	}
	if m.CursorIndex() != before {
		t.Fatalf("sidebar cursor moved to %q on a chrome click", m.CursorIndex())
	}
}

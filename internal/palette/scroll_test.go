package palette

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// scroll_test.go covers the palette's two scroll windows (#2041): the main
// list's `top`, the projects column's `sideTop`, and the one-entry scrolloff
// both of them navigate with.

// pruneProjectMsg is the stand-in aux msg of a projects-column row (removing
// it from the history) — used to check the aux action still hits the row the
// user sees after the column has scrolled.
type pruneProjectMsg struct{ Path string }

// manyProjectsMode builds a recent mode with n numbered projects in the
// column, each carrying an activation and an aux msg.
func manyProjectsMode(files []string, n int) *RecentMode {
	m := testRecentMode(files)
	m.SetProjects(func() []Item {
		out := make([]Item, n)
		for i := range out {
			path := fmt.Sprintf("/p/proj%02d", i)
			out[i] = Item{
				Title: fmt.Sprintf("proj%02d", i),
				Msg:   projectsMsg{Path: path},
				Aux:   pruneProjectMsg{Path: path},
			}
		}
		return out
	})
	return m
}

// numberedFiles is a fixed MRU list of n files for the main result list.
func numberedFiles(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("file%02d.go", i)
	}
	return out
}

// TestPaletteMainListScrollOff (#2041): moving down the main list advances the
// window one entry before the cursor hits the bottom row, so the next entry is
// always visible.
func TestPaletteMainListScrollOff(t *testing.T) {
	p := New(Config{MaxResults: 5}, fileMode(numberedFiles(10)...))
	p.SetSize(120, 40)
	p.Open(Context{Root: "."})
	if len(p.items) != 10 {
		t.Fatalf("got %d items, want 10", len(p.items))
	}
	// Rows 0..4 are visible: down to row 3 still keeps row 4 below the cursor.
	for i := 0; i < 3; i++ {
		p.Update(downKey())
	}
	if p.selected != 3 || p.top != 0 {
		t.Fatalf("selected=%d top=%d, want 3/0 (no scroll while a row is left below)", p.selected, p.top)
	}
	// The next step reaches the last visible row, so the window moves on and
	// keeps one entry below the selection.
	p.Update(downKey())
	if p.selected != 4 || p.top != 1 {
		t.Fatalf("selected=%d top=%d, want 4/1 (scrolloff advances the window)", p.selected, p.top)
	}
	// Upwards is symmetric: the window follows one entry before the top row.
	p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.selected != 3 || p.top != 1 {
		t.Fatalf("selected=%d top=%d, want 3/1", p.selected, p.top)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.selected != 2 || p.top != 1 {
		t.Fatalf("selected=%d top=%d, want 2/1 (row 1 still above the cursor)", p.selected, p.top)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if p.selected != 1 || p.top != 0 {
		t.Fatalf("selected=%d top=%d, want 1/0", p.selected, p.top)
	}
}

// TestPaletteMainListScrollOffAtEnds (#2041): the very first and last entries
// still sit flush against the window edge — the margin never scrolls an empty
// row into view.
func TestPaletteMainListScrollOffAtEnds(t *testing.T) {
	p := New(Config{MaxResults: 5}, fileMode(numberedFiles(10)...))
	p.SetSize(120, 40)
	p.Open(Context{Root: "."})
	for i := 0; i < 9; i++ {
		p.Update(downKey())
	}
	if p.selected != 9 || p.top != 5 {
		t.Fatalf("selected=%d top=%d, want the last row with top=5", p.selected, p.top)
	}
	// One more step wraps to the first entry and rewinds the window fully.
	p.Update(downKey())
	if p.selected != 0 || p.top != 0 {
		t.Fatalf("selected=%d top=%d, want the wrap back to 0/0", p.selected, p.top)
	}
	// A list shorter than the window never scrolls.
	short := New(Config{MaxResults: 5}, fileMode(numberedFiles(3)...))
	short.SetSize(120, 40)
	short.Open(Context{Root: "."})
	for i := 0; i < 5; i++ {
		short.Update(downKey())
		if short.top != 0 {
			t.Fatalf("short list must not scroll, top=%d after %d steps", short.top, i+1)
		}
	}
}

// TestPaletteSideColumnScrolls (#2041): every projects-column entry is
// reachable by keyboard — the column window follows the selection, with the
// same one-entry scrolloff as the main list.
func TestPaletteSideColumnScrolls(t *testing.T) {
	m := manyProjectsMode([]string{"a.go", "b.go"}, 8)
	p := New(Config{MaxResults: 4}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	if got := p.sideVisibleRows(); got != 3 {
		t.Fatalf("sideVisibleRows = %d, want 3 (window minus the heading)", got)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !p.sideFocus {
		t.Fatal("tab must focus the projects column")
	}
	// Rows 0..2 are visible; the step onto row 2 already advances the window.
	p.Update(downKey())
	if p.sideSel != 1 || p.sideTop != 0 {
		t.Fatalf("sideSel=%d sideTop=%d, want 1/0", p.sideSel, p.sideTop)
	}
	p.Update(downKey())
	if p.sideSel != 2 || p.sideTop != 1 {
		t.Fatalf("sideSel=%d sideTop=%d, want 2/1 (scrolloff advances the column)", p.sideSel, p.sideTop)
	}
	// Walking to the last entry keeps it reachable and rendered.
	for p.sideSel < 7 {
		p.Update(downKey())
	}
	if p.sideTop != 5 {
		t.Fatalf("sideTop = %d, want 5 with the last of 8 projects selected", p.sideTop)
	}
	v := p.View()
	if !strings.Contains(v, "proj07") {
		t.Fatalf("the last project must be rendered after scrolling:\n%s", v)
	}
	if strings.Contains(v, "proj00") {
		t.Fatalf("the scrolled-off first project must be gone:\n%s", v)
	}
	// Enter activates the scrolled-to row, not the one at the window top.
	cmd := p.Update(enterKey())
	if got, ok := cmd().(projectsMsg); !ok || got.Path != "/p/proj07" {
		t.Fatalf("activation msg = %#v, want proj07", cmd())
	}
}

// TestPaletteSideColumnShortListDoesNotScroll (#2041): fewer projects than
// rows leaves the window pinned at the top, wrap-around included.
func TestPaletteSideColumnShortListDoesNotScroll(t *testing.T) {
	m := manyProjectsMode([]string{"a.go"}, 2)
	p := New(Config{MaxResults: 6}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	for i := 0; i < 5; i++ {
		p.Update(downKey())
		if p.sideTop != 0 {
			t.Fatalf("short column must not scroll, sideTop=%d after %d steps", p.sideTop, i+1)
		}
	}
}

// TestPaletteSideColumnPaging (#2041): pgdown/pgup jump the column by its own
// window height and drag the window along.
func TestPaletteSideColumnPaging(t *testing.T) {
	m := manyProjectsMode([]string{"a.go"}, 12)
	p := New(Config{MaxResults: 4}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	p.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if p.sideSel != 3 {
		t.Fatalf("sideSel = %d, want a 3-row page jump", p.sideSel)
	}
	if p.sideTop == 0 {
		t.Fatal("a page jump must move the column window")
	}
	if p.sideSel >= p.sideTop+p.sideVisibleRows() || p.sideSel < p.sideTop {
		t.Fatalf("selection %d outside the window [%d,%d)", p.sideSel, p.sideTop, p.sideTop+p.sideVisibleRows())
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if p.sideSel != 0 || p.sideTop != 0 {
		t.Fatalf("sideSel=%d sideTop=%d, want the page jump back to the top", p.sideSel, p.sideTop)
	}
}

// TestPaletteSideColumnAuxAfterScroll (#2041): both the keyboard aux chord and
// a click on a row's "✕" zone hit the entry the user sees once the column has
// scrolled — the click maps through the window top.
func TestPaletteSideColumnAuxAfterScroll(t *testing.T) {
	m := manyProjectsMode([]string{"a.go"}, 8)
	p := New(Config{MaxResults: 4}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	for p.sideSel < 6 {
		p.Update(downKey())
	}
	if p.sideTop != 5 {
		t.Fatalf("sideTop = %d, want 5", p.sideTop)
	}
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModShift})
	if cmd == nil {
		t.Fatal("shift+delete on a project row must emit its aux msg")
	}
	if got, ok := cmd().(pruneProjectMsg); !ok || got.Path != "/p/proj06" {
		t.Fatalf("aux msg = %#v, want proj06", cmd())
	}
	// A click on the first rendered row's "✕" zone targets the window top's
	// entry (proj05), not the list's first entry.
	sideW := sideWidth(p.boxWidth() - 4)
	cmd = p.Click(2+sideW-1, 1+3)
	if cmd == nil {
		t.Fatal("a click on the aux zone must emit the row's aux msg")
	}
	if got, ok := cmd().(pruneProjectMsg); !ok || got.Path != "/p/proj05" {
		t.Fatalf("clicked aux msg = %#v, want proj05", cmd())
	}
	if !p.IsOpen() {
		t.Fatal("an aux click keeps the palette open")
	}
}

// TestPaletteSideColumnClickBelowWindowIsInert (#2041): the column is shorter
// than the main list, so a click beside a file row below the column's last
// rendered project must not activate an off-screen entry.
func TestPaletteSideColumnClickBelowWindowIsInert(t *testing.T) {
	m := manyProjectsMode(numberedFiles(10), 8)
	p := New(Config{MaxResults: 4}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	// y = 1 + 3 + sideVisibleRows() is the first line past the column's rows.
	if cmd := p.Click(3, 1+3+p.sideVisibleRows()); cmd != nil {
		t.Fatalf("a click past the column window must be inert, got %#v", cmd())
	}
	if !p.IsOpen() {
		t.Fatal("an inert click must not close the palette")
	}
}

// TestPaletteWheelScrollsColumnUnderCursor (#2041): the wheel moves the column
// the pointer is over — the projects column on the left, the file list on the
// right — taking the column focus with it, and clamps at the ends.
func TestPaletteWheelScrollsColumnUnderCursor(t *testing.T) {
	m := manyProjectsMode(numberedFiles(10), 8)
	p := New(Config{MaxResults: 4}, m, fileMode())
	p.SetSize(120, 40)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	sideW := sideWidth(p.boxWidth() - 4)

	p.Wheel(3, 4, 3) // over the projects column
	if !p.sideFocus {
		t.Fatal("a wheel over the projects column must focus it")
	}
	if p.sideSel != 3 || p.sideTop != 2 {
		t.Fatalf("sideSel=%d sideTop=%d, want 3/2", p.sideSel, p.sideTop)
	}
	p.Wheel(3, 4, 9) // clamps at the last project
	if p.sideSel != 7 || p.sideTop != 5 {
		t.Fatalf("sideSel=%d sideTop=%d, want the clamped end 7/5", p.sideSel, p.sideTop)
	}
	p.Wheel(3, 4, -20) // clamps back at the first
	if p.sideSel != 0 || p.sideTop != 0 {
		t.Fatalf("sideSel=%d sideTop=%d, want the clamped start 0/0", p.sideSel, p.sideTop)
	}

	p.Wheel(2+sideW+3, 4, 3) // over the main list
	if p.sideFocus {
		t.Fatal("a wheel over the file list must move the focus back to it")
	}
	if p.selected != 3 || p.top != 1 {
		t.Fatalf("selected=%d top=%d, want 3/1", p.selected, p.top)
	}
	p.Wheel(2+sideW+3, 4, 3)
	if p.selected != 6 || p.top != 4 {
		t.Fatalf("selected=%d top=%d, want 6/4", p.selected, p.top)
	}
}

// TestPaletteWheelWithoutSideColumn (#2041): a plain palette scrolls its only
// list wherever the pointer is, and a closed palette ignores the wheel.
func TestPaletteWheelWithoutSideColumn(t *testing.T) {
	p := New(Config{MaxResults: 5}, fileMode(numberedFiles(10)...))
	p.SetSize(120, 40)
	p.Open(Context{Root: "."})
	p.Wheel(4, 4, 4)
	if p.selected != 4 || p.top != 1 {
		t.Fatalf("selected=%d top=%d, want 4/1", p.selected, p.top)
	}
	p.Close()
	p.Wheel(4, 4, 4)
	if p.selected != 4 {
		t.Fatalf("a closed palette must ignore the wheel, selected=%d", p.selected)
	}
}

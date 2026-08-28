package archview

import (
	"strconv"
	"testing"
	"time"

	"ike/internal/ui"
)

// clockPane builds a pane over p whose double-click clock the test drives.
func clockPane(t *testing.T, p string, w, h int) (Model, *time.Time) {
	t.Helper()
	m := New("archive", p, nil)
	m.SetSize(w, h)
	m.SetFocused(true)
	now := time.Unix(0, 0)
	m.now = func() time.Time { return now }
	return m, &now
}

// manyNames is a fixture of n flat file entries, enough to scroll.
func manyNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "f" + strconv.Itoa(i) + ".txt"
	}
	return out
}

// TestWheelScrollsAndClamps: the wheel moves the visible window and stops at
// both ends, dragging the cursor along so it stays visible.
func TestWheelScrollsAndClamps(t *testing.T) {
	p := writeArchive(t, manyNames(20)...)
	m := newPane(t, p)
	m.SetSize(80, 8) // bodyHeight 6
	if m.top != 0 {
		t.Fatalf("top starts at %d", m.top)
	}
	m.Wheel(3)
	if m.top != 3 {
		t.Fatalf("wheel down: top = %d, want 3", m.top)
	}
	if m.Cursor() < m.top || m.Cursor() >= m.top+6 {
		t.Fatalf("cursor %d left the window [%d,%d)", m.Cursor(), m.top, m.top+6)
	}
	// Down clamps at the last page: 20 rows, 6 visible.
	m.Wheel(100)
	if m.top != 14 {
		t.Fatalf("clamped top = %d, want 14", m.top)
	}
	if m.Cursor() != 14 {
		t.Fatalf("cursor = %d, want the first visible row 14", m.Cursor())
	}
	// Up clamps at the top.
	m.Wheel(-100)
	if m.top != 0 {
		t.Fatalf("clamped top = %d, want 0", m.top)
	}
	if m.Cursor() != 5 {
		t.Fatalf("cursor = %d, want the last visible row 5", m.Cursor())
	}
}

// TestWheelNoOpWhenEverythingFits: with fewer rows than the body, the wheel
// has nowhere to go.
func TestWheelNoOpWhenEverythingFits(t *testing.T) {
	p := writeArchive(t, "a.txt", "b.txt")
	m := newPane(t, p)
	m.Wheel(5)
	if m.top != 0 {
		t.Fatalf("top = %d, want 0", m.top)
	}
}

// TestClickSelectsRow: a click puts the cursor on the row under the pointer;
// the header, the footer and empty space below the rows are inert.
func TestClickSelectsRow(t *testing.T) {
	p := writeArchive(t, "README.md", "src/main.go")
	m := newPane(t, p)
	// rows: src, src/main.go, README.md
	if cmd := m.Click(20, 3); cmd != nil {
		t.Fatal("a single click must not activate")
	}
	if got := m.RowName(m.Cursor()); got != "README.md" {
		t.Fatalf("click landed on %q", got)
	}
	m.Click(20, 2)
	if got := m.RowName(m.Cursor()); got != "src/main.go" {
		t.Fatalf("click landed on %q", got)
	}
	// The header row keeps the selection where it was.
	m.Click(20, 0)
	if got := m.RowName(m.Cursor()); got != "src/main.go" {
		t.Fatalf("a header click moved the cursor to %q", got)
	}
	// Below the last row: nothing to select.
	m.Click(20, 10)
	if got := m.RowName(m.Cursor()); got != "src/main.go" {
		t.Fatalf("an empty-space click moved the cursor to %q", got)
	}
}

// TestClickScrolledRowHitTest: the hit test uses the same top offset the
// renderer draws with.
func TestClickScrolledRowHitTest(t *testing.T) {
	p := writeArchive(t, manyNames(20)...)
	m := newPane(t, p)
	m.SetSize(80, 8)
	m.Wheel(4)
	m.Click(20, 1)
	if got := m.RowName(m.Cursor()); got != m.RowName(4) {
		t.Fatalf("first visible row = %q, want %q", got, m.RowName(4))
	}
}

// TestDoubleClickOpensFile: a second click on the same file row within the
// window emits the open command, exactly like enter.
func TestDoubleClickOpensFile(t *testing.T) {
	p := writeArchive(t, "README.md", "src/main.go")
	m, now := clockPane(t, p, 80, 20)
	// rows: src, src/main.go, README.md — click the file twice.
	if cmd := m.Click(20, 2); cmd != nil {
		t.Fatal("the first click must not open")
	}
	*now = now.Add(100 * time.Millisecond)
	cmd := m.Click(20, 2)
	if cmd == nil {
		t.Fatal("a double click on a file must emit an open command")
	}
	msg, ok := cmd().(OpenEntryMsg)
	if !ok {
		t.Fatalf("emitted %T", cmd())
	}
	if msg.Archive != p || msg.Entry != "src/main.go" {
		t.Fatalf("open msg = %+v", msg)
	}
	// A third click starts a fresh pair instead of re-opening.
	*now = now.Add(100 * time.Millisecond)
	if cmd := m.Click(20, 2); cmd != nil {
		t.Fatal("the click after an activation must not open again")
	}
}

// TestSlowSecondClickDoesNotOpen: two clicks further apart than the window are
// two selections, not an activation.
func TestSlowSecondClickDoesNotOpen(t *testing.T) {
	p := writeArchive(t, "README.md", "src/main.go")
	m, now := clockPane(t, p, 80, 20)
	m.Click(20, 2)
	*now = now.Add(2 * ui.DoubleClickWindow)
	if cmd := m.Click(20, 2); cmd != nil {
		t.Fatal("a slow second click must not open")
	}
}

// TestDoubleClickOnOtherRowDoesNotOpen: the pair must land on the same row.
func TestDoubleClickOnOtherRowDoesNotOpen(t *testing.T) {
	p := writeArchive(t, "README.md", "src/main.go")
	m, now := clockPane(t, p, 80, 20)
	m.Click(20, 2)
	*now = now.Add(50 * time.Millisecond)
	if cmd := m.Click(20, 3); cmd != nil {
		t.Fatal("clicks on different rows must not open")
	}
}

// TestFoldGlyphClickTogglesDirectory: a single click on the glyph folds and
// unfolds, while a click on the name only selects.
func TestFoldGlyphClickTogglesDirectory(t *testing.T) {
	p := writeArchive(t, "README.md", "src/main.go")
	m, now := clockPane(t, p, 80, 20)
	// row 0 is "src" at depth 0: the glyph sits at x 1..2.
	m.Click(1, 1)
	if got := rowNames(&m); !equal(got, []string{"src", "README.md"}) {
		t.Fatalf("the glyph click did not collapse: %v", got)
	}
	if m.RowName(m.Cursor()) != "src" {
		t.Fatalf("the glyph click left the cursor on %q", m.RowName(m.Cursor()))
	}
	m.Click(2, 1)
	if got := rowNames(&m); !equal(got, []string{"src", "src/main.go", "README.md"}) {
		t.Fatalf("the second glyph click did not expand: %v", got)
	}
	// The name only selects.
	*now = now.Add(time.Second)
	m.Click(6, 1)
	if got := rowNames(&m); !equal(got, []string{"src", "src/main.go", "README.md"}) {
		t.Fatalf("a name click folded: %v", got)
	}
}

// TestDoubleClickOnDirectoryFolds: away from the glyph, a directory still
// folds on the double-click — the enter path.
func TestDoubleClickOnDirectoryFolds(t *testing.T) {
	p := writeArchive(t, "README.md", "src/main.go")
	m, now := clockPane(t, p, 80, 20)
	m.Click(6, 1)
	*now = now.Add(50 * time.Millisecond)
	if cmd := m.Click(6, 1); cmd != nil {
		t.Fatal("a directory must not emit an open command")
	}
	if got := rowNames(&m); !equal(got, []string{"src", "README.md"}) {
		t.Fatalf("the double click did not fold: %v", got)
	}
}

// TestNestedFoldGlyphOffset: the glyph zone shifts with the row's depth, so a
// nested directory's glyph is not where its parent's was.
func TestNestedFoldGlyphOffset(t *testing.T) {
	p := writeArchive(t, "src/deep/a.go")
	m, now := clockPane(t, p, 80, 20)
	// rows: src, src/deep, src/deep/a.go — "src/deep" sits at depth 1, so its
	// glyph is at x 3..4; x 1 there is indentation and only selects.
	*now = now.Add(time.Second)
	m.Click(1, 2)
	if got := rowNames(&m); !equal(got, []string{"src", "src/deep", "src/deep/a.go"}) {
		t.Fatalf("an indentation click folded: %v", got)
	}
	if m.RowName(m.Cursor()) != "src/deep" {
		t.Fatalf("cursor on %q", m.RowName(m.Cursor()))
	}
	m.Click(3, 2)
	if got := rowNames(&m); !equal(got, []string{"src", "src/deep"}) {
		t.Fatalf("the nested glyph click did not fold: %v", got)
	}
}

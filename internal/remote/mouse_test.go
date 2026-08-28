package remote

import (
	"strconv"
	"testing"
	"time"

	"ike/internal/ui"
)

// clockPane is a mounted pane whose double-click clock the test drives.
func clockPane(t *testing.T, conn *fakeConn) (*Model, *time.Time) {
	t.Helper()
	m := mounted(t, conn)
	now := time.Unix(0, 0)
	m.now = func() time.Time { return now }
	return m, &now
}

// bigHost is a fixture with enough flat entries under / to scroll.
func bigHost(n int) *fakeConn {
	var entries []Entry
	for i := 0; i < n; i++ {
		entries = append(entries, file("f"+strconv.Itoa(i)+".txt"))
	}
	return &fakeConn{
		home: "/",
		dirs: map[string][]Entry{"/": entries},
	}
}

// TestClickSelectsRow guards the single click: the row under the pointer
// becomes the selection and nothing is activated.
func TestClickSelectsRow(t *testing.T) {
	m, _ := clockPane(t, host())
	// Rows: /etc, /home, /home/user, /home/user/logs, /home/user/notes.txt —
	// y 1 is the first, so y 5 is notes.txt.
	if cmd := m.Click(4, 5); cmd != nil {
		t.Fatalf("single click must only select, got a command")
	}
	if got := m.RowPath(m.Cursor()); got != "/home/user/notes.txt" {
		t.Fatalf("cursor on %q, want /home/user/notes.txt", got)
	}
}

// TestDoubleClickOpensFile guards the activation gesture: a second click on
// the selected file row emits the open message enter emits.
func TestDoubleClickOpensFile(t *testing.T) {
	m, now := clockPane(t, host())
	m.Click(4, 5)
	*now = now.Add(100 * time.Millisecond)
	cmd := m.Click(4, 5)
	if cmd == nil {
		t.Fatal("double click must activate the row")
	}
	msg, ok := cmd().(OpenFileMsg)
	if !ok || msg.Path != "/home/user/notes.txt" {
		t.Fatalf("msg = %#v, want OpenFileMsg for notes.txt", msg)
	}
	// A third click must not re-open: the tracker was cleared on activation.
	*now = now.Add(50 * time.Millisecond)
	if cmd := m.Click(4, 5); cmd != nil {
		t.Fatal("the click after an activation must only select")
	}
}

// TestSlowSecondClickOnlySelects guards the window: two clicks further apart
// than ui.DoubleClickWindow are two single clicks.
func TestSlowSecondClickOnlySelects(t *testing.T) {
	m, now := clockPane(t, host())
	m.Click(4, 5)
	*now = now.Add(ui.DoubleClickWindow + time.Millisecond)
	if cmd := m.Click(4, 5); cmd != nil {
		t.Fatal("a slow second click must not activate")
	}
}

// TestClickOnFoldGlyphTogglesDirectory guards the caret: one click on a
// directory's glyph folds it, without waiting for a second.
func TestClickOnFoldGlyphTogglesDirectory(t *testing.T) {
	m, _ := clockPane(t, host())
	// /home is row 2 (y 2) at depth 0: its glyph occupies x 1-2.
	if got := m.RowPath(1); got != "/home" {
		t.Fatalf("row 1 = %q, want /home", got)
	}
	pump(t, m, m.Click(1, 2))
	for _, p := range rowPaths(m) {
		if p == "/home/user" {
			t.Fatalf("glyph click did not collapse /home: %v", rowPaths(m))
		}
	}
}

// TestClickOnChromeIsInert guards the hit-test: the header line, the footer
// and the blank tail below the last row select nothing.
func TestClickOnChromeIsInert(t *testing.T) {
	m, _ := clockPane(t, host())
	before := m.Cursor()
	for _, y := range []int{0, 6, 11, 20} {
		if cmd := m.Click(4, y); cmd != nil {
			t.Fatalf("click at y %d must be inert", y)
		}
	}
	if m.Cursor() != before {
		t.Fatalf("cursor moved to %d from %d on a chrome click", m.Cursor(), before)
	}
}

// TestWheelScrollsAndClampsAtLastPage guards the wheel: it scrolls the list,
// drags the cursor inside the window, and never scrolls the last page off.
func TestWheelScrollsAndClampsAtLastPage(t *testing.T) {
	m, _ := clockPane(t, bigHost(40))
	m.SetSize(60, 12) // body height 10
	m.Wheel(3)
	if m.top != 3 {
		t.Fatalf("top = %d, want 3", m.top)
	}
	if m.Cursor() < m.top || m.Cursor() >= m.top+m.bodyHeight() {
		t.Fatalf("cursor %d left the window [%d, %d)", m.Cursor(), m.top, m.top+m.bodyHeight())
	}
	m.Wheel(1000)
	if want := 40 - m.bodyHeight(); m.top != want {
		t.Fatalf("top = %d, want clamped to %d", m.top, want)
	}
	m.Wheel(-1000)
	if m.top != 0 {
		t.Fatalf("top = %d, want clamped to 0", m.top)
	}
}

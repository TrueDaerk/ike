package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	"ike/internal/hscroll"
)

// hMarkLoaded loads content from a temp file into a sized, focused, gutterless
// pane — the plain seam the other view tests use.
func hMarkLoaded(t *testing.T, content string, w, h int) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wide.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(w, h)
	m.view.LineNumbers = false
	m.SetFocused(true)
	return m
}

// hMarkModel is a sized, focused buffer: one line far wider than the pane and
// one that fits comfortably inside it.
func hMarkModel(t *testing.T) Model {
	t.Helper()
	m := hMarkLoaded(t, strings.Repeat("abcdefghij", 8)+"\nshort\n", 30, 6)
	// Park the caret out of the way: a mark never covers the caret's cell, and
	// these cases are about the marks themselves.
	m.cursor = buffer.Position{Line: 1, Col: 3}
	return m
}

// rowOf returns the plain (ANSI-stripped) rendered row at index i.
func rowOf(m Model, i int) string {
	rows := strings.Split(plainView(m), "\n")
	if i >= len(rows) {
		return ""
	}
	return rows[i]
}

// TestHMarkNoneAtOffsetZeroOnShortLines (#2377): an unscrolled view of a line
// that fits carries no mark at all — the display must not become noise.
func TestHMarkNoneAtOffsetZeroOnShortLines(t *testing.T) {
	m := hMarkModel(t)
	row := rowOf(m, 1) // "short"
	if strings.Contains(row, hscroll.LeftGlyph) {
		t.Fatalf("offset 0 must not mark the left edge: %q", row)
	}
	if strings.Contains(row, hscroll.RightGlyph) {
		t.Fatalf("a line inside the window must not mark the right edge: %q", row)
	}
}

// TestHMarkRightOnOverflowingLine (#2377): a line running past the window says
// so at its right edge, even unscrolled.
func TestHMarkRightOnOverflowingLine(t *testing.T) {
	m := hMarkModel(t)
	row := rowOf(m, 0)
	if !strings.HasSuffix(row, hscroll.RightGlyph) {
		t.Fatalf("an overflowing line must mark the right edge: %q", row)
	}
	if strings.Contains(row, hscroll.LeftGlyph) {
		t.Fatalf("offset 0 must not mark the left edge: %q", row)
	}
}

// TestHMarkLeftWhenScrolled (#2377): scrolling right marks the left edge of
// every row, including the short one — the window, not the line, moved.
func TestHMarkLeftWhenScrolled(t *testing.T) {
	m := hMarkModel(t)
	m.ScrollXBy(10)
	if m.view.Left != 10 {
		t.Fatalf("precondition: offset %d, want 10", m.view.Left)
	}
	for i, want := range []bool{true, true} {
		row := rowOf(m, i)
		if got := strings.HasPrefix(row, hscroll.LeftGlyph); got != want {
			t.Fatalf("row %d: left mark %v, want %v: %q", i, got, want, row)
		}
	}
	// The short line ends inside the window, so it keeps its right edge clean.
	if row := rowOf(m, 1); strings.Contains(row, hscroll.RightGlyph) {
		t.Fatalf("a line ending inside the window must not mark the right edge: %q", row)
	}
}

// TestHMarkNoneUnderSoftWrap (#2377): soft wrap has no horizontal scroll (#64)
// and no segment runs off the edge, so nothing renders.
func TestHMarkNoneUnderSoftWrap(t *testing.T) {
	m := hMarkModel(t)
	m.ScrollXBy(10) // ignored under wrap, but assert the state either way
	m.softWrap = true
	m.view.Left = 10
	m.bumpRender()
	v := plainView(m)
	if strings.Contains(v, hscroll.LeftGlyph) || strings.Contains(v, hscroll.RightGlyph) {
		t.Fatalf("soft wrap must render no edge marks:\n%s", v)
	}
}

// TestHMarkKeepsRowWidth (#2377): the marks overlay edge cells, they never add
// a column — the rendered row width is identical with and without them.
func TestHMarkKeepsRowWidth(t *testing.T) {
	m := hMarkModel(t)
	m.ScrollXBy(10)
	with := ansi.StringWidth(rowOf(m, 0))
	m.hMarks = false
	m.bumpRender()
	without := ansi.StringWidth(rowOf(m, 0))
	if with != without {
		t.Fatalf("marks changed the row width: %d with, %d without", with, without)
	}
}

// TestHMarkNeverCoversTheCaret (#2377): the one cell a mark may not take is
// the caret's — the cursor has to stay where the user put it.
func TestHMarkNeverCoversTheCaret(t *testing.T) {
	m := hMarkModel(t)
	m.ScrollXBy(10)
	m.cursor = buffer.Position{Line: 0, Col: 10} // the window's first column
	m.bumpRender()
	if row := rowOf(m, 0); strings.HasPrefix(row, hscroll.LeftGlyph) {
		t.Fatalf("the left mark must yield to the caret on its cell: %q", row)
	}
	// A caret elsewhere on the line leaves the mark alone.
	m.cursor = buffer.Position{Line: 0, Col: 15}
	m.bumpRender()
	if row := rowOf(m, 0); !strings.HasPrefix(row, hscroll.LeftGlyph) {
		t.Fatalf("the left mark should be back: %q", row)
	}
}

// TestHMarkConfigurable (#2377): ui.h_scroll_marks turns the marks off, and
// the rows render exactly as they did before the marks existed.
func TestHMarkConfigurable(t *testing.T) {
	m := hMarkModel(t)
	m.Configure(host.MapConfig{"ui.h_scroll_marks": "false"})
	m.ScrollXBy(10)
	v := plainView(m)
	if strings.Contains(v, hscroll.LeftGlyph) || strings.Contains(v, hscroll.RightGlyph) {
		t.Fatalf("ui.h_scroll_marks=false must render no marks:\n%s", v)
	}
	m.Configure(host.MapConfig{"ui.h_scroll_marks": "true"})
	m.bumpRender()
	if !strings.Contains(plainView(m), hscroll.LeftGlyph) {
		t.Fatal("ui.h_scroll_marks=true must bring the marks back")
	}
}

// TestHMarkClearsTheScrollbarColumn (#2377): with the vertical bar up, the
// right mark sits one column left of it instead of underneath it.
func TestHMarkClearsTheScrollbarColumn(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString(strings.Repeat("abcdefghij", 8) + "\n")
	}
	m := hMarkLoaded(t, b.String(), 30, 6)
	if _, _, _, _, ok := m.scrollbarGeometry(); !ok {
		t.Fatal("precondition: the buffer must overflow the viewport")
	}
	row := rowOf(m, 0)
	at := strings.Index(row, hscroll.RightGlyph)
	if at < 0 {
		t.Fatalf("an overflowing line must mark the right edge: %q", row)
	}
	if cell := ansi.StringWidth(row[:at]); cell != m.width-2 {
		t.Fatalf("right mark at cell %d, want %d (one left of the scrollbar)", cell, m.width-2)
	}
}

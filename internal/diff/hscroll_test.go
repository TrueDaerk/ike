package diff

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/hscroll"
)

// hMarkModel pairs a line far wider than the pane with one that fits, on both
// sides, so a single view answers both edge questions.
func hMarkModel(t *testing.T) *Model {
	t.Helper()
	wide := strings.Repeat("abcdefghij", 8)
	m := testModel(t, "short\n"+wide, "short\n"+wide)
	m.SetSize(40, 10)
	return m
}

// TestHMarkNoneAtOffsetZeroOnShortLines (#2377): an unscrolled row that fits
// carries no mark — neither edge hides anything.
func TestHMarkNoneAtOffsetZeroOnShortLines(t *testing.T) {
	m := hMarkModel(t)
	row := strings.Split(plainView(m), "\n")[0] // "short" on both sides
	if strings.Contains(row, hscroll.LeftGlyph) || strings.Contains(row, hscroll.RightGlyph) {
		t.Fatalf("a short unscrolled row must carry no marks: %q", row)
	}
}

// TestHMarkRightOnOverflowingRow (#2377): a row running past its column says
// so at that column's right edge — on both sides of a side-by-side diff.
func TestHMarkRightOnOverflowingRow(t *testing.T) {
	m := hMarkModel(t)
	row := strings.Split(plainView(m), "\n")[1]
	left, right, ok := strings.Cut(row, "│")
	if !ok {
		t.Fatalf("expected a side-by-side row: %q", row)
	}
	if !strings.HasSuffix(strings.TrimRight(left, " "), hscroll.RightGlyph) ||
		!strings.HasSuffix(strings.TrimRight(right, " "), hscroll.RightGlyph) {
		t.Fatalf("both overflowing columns must mark their right edge: %q", row)
	}
	if strings.Contains(row, hscroll.LeftGlyph) {
		t.Fatalf("offset 0 must not mark the left edge: %q", row)
	}
}

// TestHMarkLeftWhenScrolled (#2377): scrolling marks the left edge of both
// columns, and the short row still keeps its right edge clean.
func TestHMarkLeftWhenScrolled(t *testing.T) {
	m := hMarkModel(t)
	m.ScrollXBy(5)
	if m.HOffset() != 5 {
		t.Fatalf("precondition: offset %d, want 5", m.HOffset())
	}
	rows := strings.Split(plainView(m), "\n")
	if n := strings.Count(rows[1], hscroll.LeftGlyph); n != 2 {
		t.Fatalf("both columns must mark the left edge, got %d: %q", n, rows[1])
	}
	// "short" is now entirely left of the window: nothing continues right.
	if strings.Contains(rows[0], hscroll.RightGlyph) {
		t.Fatalf("an exhausted row must not mark the right edge: %q", rows[0])
	}
}

// TestHMarkKeepsColumnWidths (#2377): the marks overlay cells, so the rendered
// row width — and with it the column separator's position — never moves.
func TestHMarkKeepsColumnWidths(t *testing.T) {
	m := hMarkModel(t)
	m.ScrollXBy(5)
	with := strings.Split(plainView(m), "\n")
	m.SetHScrollMarks(false)
	without := strings.Split(plainView(m), "\n")
	for i := range with {
		if a, b := ansi.StringWidth(with[i]), ansi.StringWidth(without[i]); a != b {
			t.Fatalf("row %d: width %d with marks, %d without", i, a, b)
		}
	}
}

// TestHMarkToggle (#2377): SetHScrollMarks turns the marks off and back on,
// re-rendering each time (ui.h_scroll_marks reaches the model through it).
func TestHMarkToggle(t *testing.T) {
	m := hMarkModel(t)
	m.ScrollXBy(5)
	m.SetHScrollMarks(false)
	if v := plainView(m); strings.Contains(v, hscroll.LeftGlyph) || strings.Contains(v, hscroll.RightGlyph) {
		t.Fatalf("marks off must render nothing:\n%s", v)
	}
	m.SetHScrollMarks(true)
	if v := plainView(m); !strings.Contains(v, hscroll.LeftGlyph) {
		t.Fatalf("marks on must render again:\n%s", v)
	}
}

// TestHMarkNoneOnGapRows (#2377): the blank counterpart of a one-sided row is
// not content, so it carries no marks however far the view is scrolled.
func TestHMarkNoneOnGapRows(t *testing.T) {
	wide := strings.Repeat("abcdefghij", 8)
	m := testModel(t, "", wide)
	m.SetSize(40, 10)
	m.ScrollXBy(5)
	row := strings.Split(plainView(m), "\n")[0]
	left, _, ok := strings.Cut(row, "│")
	if !ok {
		t.Fatalf("expected a side-by-side row: %q", row)
	}
	if strings.ContainsAny(left, hscroll.LeftGlyph+hscroll.RightGlyph) {
		t.Fatalf("a gap column must stay blank: %q", row)
	}
}

package explorer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
	"ike/internal/hscroll"
)

// hMarkTree mounts a tree holding one name far wider than the pane and one
// that fits, in a window narrow enough to overflow.
func hMarkTree(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "this_is_a_very_long_file_name_that_overflows.txt"), "x")
	mustWrite(t, filepath.Join(root, "a.txt"), "x")
	return mounted(t, root, 20, 10)
}

// rowWith returns the first rendered row containing probe.
func rowWith(t *testing.T, m Model, probe string) string {
	t.Helper()
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, probe) {
			return l
		}
	}
	t.Fatalf("no row contains %q:\n%s", probe, ansi.Strip(m.View()))
	return ""
}

// TestHMarkNoneAtOffsetZeroOnShortRows (#2377): a row that fits the window
// carries no mark while the tree is unscrolled.
func TestHMarkNoneAtOffsetZeroOnShortRows(t *testing.T) {
	m := hMarkTree(t)
	row := rowWith(t, m, "a.txt")
	if strings.Contains(row, hscroll.LeftGlyph) || strings.Contains(row, hscroll.RightGlyph) {
		t.Fatalf("a short unscrolled row must carry no marks: %q", row)
	}
}

// TestHMarkRightOnClippedRow (#2377): a name running past the window says so
// at the cell it was cut at.
func TestHMarkRightOnClippedRow(t *testing.T) {
	m := hMarkTree(t)
	row := rowWith(t, m, "this_is_a")
	if !strings.Contains(row, hscroll.RightGlyph) {
		t.Fatalf("a clipped row must mark the right edge: %q", row)
	}
	if strings.Contains(row, hscroll.LeftGlyph) {
		t.Fatalf("offset 0 must not mark the left edge: %q", row)
	}
}

// TestHMarkLeftWhenScrolled (#2377): scrolling right marks the left edge of
// the rows, and a row that ends inside the window keeps its right edge clean.
func TestHMarkLeftWhenScrolled(t *testing.T) {
	m := hMarkTree(t)
	m.ScrollXBy(6)
	if m.offsetX != 6 {
		t.Fatalf("precondition: offsetX %d, want 6", m.offsetX)
	}
	row := rowWith(t, m, hscroll.LeftGlyph)
	if !strings.HasPrefix(row, hscroll.LeftGlyph) {
		t.Fatalf("a scrolled row must open with the left mark: %q", row)
	}
	// "a.txt" is entirely left of the window now — blank, but still marked
	// left, and never marked right.
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, "a.txt") && strings.Contains(l, hscroll.RightGlyph) {
			t.Fatalf("an exhausted row must not mark the right edge: %q", l)
		}
	}
}

// TestHMarkKeepsRowWidth (#2377): the marks overlay cells; the rendered rows
// keep the width they had without them, so the scrollbar column stays put.
func TestHMarkKeepsRowWidth(t *testing.T) {
	m := hMarkTree(t)
	m.ScrollXBy(6)
	with := strings.Split(ansi.Strip(m.View()), "\n")
	m.hMarks = false
	without := strings.Split(ansi.Strip(m.View()), "\n")
	for i := range with {
		if a, b := ansi.StringWidth(with[i]), ansi.StringWidth(without[i]); a != b {
			t.Fatalf("row %d: width %d with marks, %d without", i, a, b)
		}
	}
}

// TestHMarkConfigurable (#2377): ui.h_scroll_marks reaches the tree through
// Configure, like the [explorer] keys.
func TestHMarkConfigurable(t *testing.T) {
	m := hMarkTree(t)
	m.Configure(host.MapConfig{"ui.h_scroll_marks": "false"})
	m.ScrollXBy(6)
	if v := ansi.Strip(m.View()); strings.Contains(v, hscroll.LeftGlyph) {
		t.Fatalf("ui.h_scroll_marks=false must render no marks:\n%s", v)
	}
	m.Configure(host.MapConfig{"ui.h_scroll_marks": "true"})
	if v := ansi.Strip(m.View()); !strings.Contains(v, hscroll.LeftGlyph) {
		t.Fatalf("ui.h_scroll_marks=true must bring the marks back:\n%s", v)
	}
}

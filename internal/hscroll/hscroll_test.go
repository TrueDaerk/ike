package hscroll

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
)

func TestCut(t *testing.T) {
	// Offset 0 never marks the left edge — the mark must not become noise.
	if l, _ := Cut(0, 20, 100); l {
		t.Fatal("offset 0 should not mark the left edge")
	}
	if l, _ := Cut(1, 20, 100); !l {
		t.Fatal("a scrolled window should mark the left edge")
	}
	// A row narrower than the window has nothing off the right edge.
	if _, r := Cut(0, 20, 20); r {
		t.Fatal("a row exactly filling the window should not mark the right edge")
	}
	if _, r := Cut(0, 20, 5); r {
		t.Fatal("a short row should not mark the right edge")
	}
	if _, r := Cut(0, 20, 21); !r {
		t.Fatal("an overflowing row should mark the right edge")
	}
	// The row width is measured from the line start, so scrolling right
	// eventually exhausts it.
	if _, r := Cut(10, 20, 30); r {
		t.Fatal("the scrolled window reaches the row end; no right mark")
	}
	if _, r := Cut(10, 20, 31); !r {
		t.Fatal("one cell past the window should mark the right edge")
	}
	// A zero-width window marks nothing on the right (there is no cell).
	if _, r := Cut(0, 0, 100); r {
		t.Fatal("a zero-width window should mark nothing")
	}
}

func TestStampKeepsWidth(t *testing.T) {
	st := lipgloss.NewStyle()
	row := strings.Repeat("x", 10)
	for _, tc := range []struct{ left, right bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		got := Stamp(row, 10, tc.left, tc.right, st)
		if w := ansi.StringWidth(got); w != 10 {
			t.Fatalf("left=%v right=%v: width %d, want 10 (%q)", tc.left, tc.right, w, got)
		}
		if tc.left != strings.HasPrefix(ansi.Strip(got), LeftGlyph) {
			t.Fatalf("left=%v: left glyph mismatch in %q", tc.left, got)
		}
		if tc.right != strings.HasSuffix(ansi.Strip(got), RightGlyph) {
			t.Fatalf("right=%v: right glyph mismatch in %q", tc.right, got)
		}
	}
}

func TestStampNoMarksIsIdentity(t *testing.T) {
	st := lipgloss.NewStyle()
	row := "hello"
	if got := Stamp(row, 20, false, false, st); got != row {
		t.Fatalf("no marks should leave the row untouched, got %q", got)
	}
	// A short row is not padded when only the left edge is marked.
	if got := ansi.Strip(Stamp(row, 20, true, false, st)); got != LeftGlyph+"ello" {
		t.Fatalf("left mark on a short row: got %q", got)
	}
}

func TestStampPadsShortRowForRightMark(t *testing.T) {
	// The right mark owns the window's last cell, so a row that does not
	// reach it is padded out — otherwise the glyph would land mid-row.
	got := ansi.Strip(Stamp("abc", 6, false, true, lipgloss.NewStyle()))
	if got != "abc  "+RightGlyph {
		t.Fatalf("got %q", got)
	}
}

func TestStampNarrowWindow(t *testing.T) {
	st := lipgloss.NewStyle()
	if got := ansi.Strip(Stamp("abc", 1, true, false, st)); got != LeftGlyph {
		t.Fatalf("one-cell window, left mark: got %q", got)
	}
	if got := Stamp("abc", 0, true, true, st); got != "abc" {
		t.Fatalf("a zero-width window should stamp nothing, got %q", got)
	}
}

func TestStampPreservesStyledContent(t *testing.T) {
	// The rows the callers hand over are ANSI-styled; stamping an edge must
	// not strip the styling of the cells it leaves alone.
	body := lipgloss.NewStyle().Bold(true).Render("abcdefgh")
	got := Stamp(body, 8, true, true, lipgloss.NewStyle())
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("styling lost: %q", got)
	}
	if plain := ansi.Strip(got); plain != LeftGlyph+"bcdefg"+RightGlyph {
		t.Fatalf("content mismatch: %q", plain)
	}
}

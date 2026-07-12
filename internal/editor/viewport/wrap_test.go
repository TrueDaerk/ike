package viewport

import (
	"reflect"
	"strings"
	"testing"
)

func TestWrapSegments(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		width    int
		tabWidth int
		want     []int
	}{
		{"empty line is one row", "", 10, 4, []int{0}},
		{"short line is one row", "abc", 10, 4, []int{0}},
		{"exact width is one row", strings.Repeat("a", 10), 10, 4, []int{0}},
		{"splits at width", strings.Repeat("a", 25), 10, 4, []int{0, 10, 20}},
		{"tab budgets tabWidth cells", "\t\t\tabc", 10, 4, []int{0, 2}},
		{"straddling tab starts next row", strings.Repeat("a", 8) + "\tbb", 10, 4, []int{0, 8}},
		{"tab at row start renders clamped", strings.Repeat("\t", 3), 2, 4, []int{0, 1, 2}},
	}
	for _, c := range cases {
		if got := WrapSegments([]rune(c.line), c.width, c.tabWidth); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: segments=%v want %v", c.name, got, c.want)
		}
	}
}

func TestSegmentIndex(t *testing.T) {
	segs := []int{0, 10, 20}
	for col, want := range map[int]int{0: 0, 9: 0, 10: 1, 19: 1, 20: 2, 25: 2, 99: 2} {
		if got := SegmentIndex(segs, col); got != want {
			t.Errorf("col %d: segment=%d want %d", col, got, want)
		}
	}
}

func TestSegmentEnd(t *testing.T) {
	segs := []int{0, 10, 20}
	if got := SegmentEnd(segs, 0, 25); got != 10 {
		t.Errorf("segment 0 end=%d want 10", got)
	}
	if got := SegmentEnd(segs, 2, 25); got != 25 {
		t.Errorf("last segment end=%d want 25 (line length)", got)
	}
}

func TestScrollWrappedFollowsCursorDown(t *testing.T) {
	v := Viewport{}
	v.SetSize(40, 6)
	rows := func(int) int { return 2 } // every line wraps to two rows
	// Cursor on line 5, second segment: visual rows 0..11, cursor row 11.
	v.ScrollWrapped(5, 1, 10, rows)
	// Window holds 6 rows; Top must have advanced so row 11 is inside.
	if v.Top != 3 {
		t.Fatalf("Top=%d want 3 (cursor visual row within the window)", v.Top)
	}
	if v.Left != 0 {
		t.Fatalf("Left=%d; wrap must pin horizontal scroll at 0", v.Left)
	}
	// Scrolling back up follows too.
	v.ScrollWrapped(0, 0, 10, rows)
	if v.Top != 0 {
		t.Fatalf("Top=%d want 0 after moving to the first line", v.Top)
	}
}

func TestScrollWrappedHonoursScrollOff(t *testing.T) {
	v := Viewport{ScrollOff: 2}
	v.SetSize(40, 8)
	rows := func(int) int { return 1 }
	v.ScrollWrapped(20, 0, 40, rows)
	// Cursor row must sit at least ScrollOff rows above the bottom.
	vr := 20 - v.Top
	if vr > 8-1-2 {
		t.Fatalf("cursor visual row %d violates scrolloff (Top=%d)", vr, v.Top)
	}
}

func TestScrollWrappedSkipsFoldedRows(t *testing.T) {
	v := Viewport{}
	v.SetSize(40, 4)
	// Lines 1..8 hidden inside a collapsed fold: they occupy no rows.
	rows := func(l int) int {
		if l >= 1 && l <= 8 {
			return 0
		}
		return 1
	}
	v.ScrollWrapped(10, 0, 12, rows)
	// Rows: line 0 (1), lines 1-8 (0), line 9 (1), cursor line 10 → visual
	// row 2 of 4; no scrolling needed.
	if v.Top != 0 {
		t.Fatalf("Top=%d want 0 (folded lines occupy no rows)", v.Top)
	}
}

func TestGutterContinuation(t *testing.T) {
	v := Viewport{LineNumbers: true}
	g := v.GutterContinuation(100)
	if len([]rune(g)) != v.GutterWidth(100) {
		t.Fatalf("continuation gutter %q width %d want %d", g, len([]rune(g)), v.GutterWidth(100))
	}
	if !strings.Contains(g, "↪") {
		t.Fatalf("continuation gutter %q lacks the wrap marker", g)
	}
	v.LineNumbers = false
	if g := v.GutterContinuation(100); g != "" {
		t.Fatalf("continuation gutter %q want empty with line numbers off", g)
	}
}

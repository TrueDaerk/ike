package codepreview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// columns_test.go covers the geometry and the two-column body every picker
// shares (#2053): Split and Cache.Columns.

// TestSplitDropsColumnWhenNarrow: below MinSplitWidth the list keeps the whole
// width and the caller renders one column.
func TestSplitDropsColumnWhenNarrow(t *testing.T) {
	for _, inner := range []int{0, 20, MinSplitWidth - 1} {
		listW, previewW := Split(inner)
		if previewW != 0 {
			t.Fatalf("Split(%d) previewW = %d, want 0", inner, previewW)
		}
		if listW != inner {
			t.Fatalf("Split(%d) listW = %d, want the whole width", inner, listW)
		}
	}
}

// TestSplitBoundsAndSum: the preview takes two fifths, clamped to
// [MinColumnWidth, MaxColumnWidth], and the two columns plus the divider
// always account for exactly the inner width.
func TestSplitBoundsAndSum(t *testing.T) {
	for inner := MinSplitWidth; inner <= 400; inner++ {
		listW, previewW := Split(inner)
		if previewW < MinColumnWidth || previewW > MaxColumnWidth {
			t.Fatalf("Split(%d) previewW = %d, outside [%d, %d]", inner, previewW, MinColumnWidth, MaxColumnWidth)
		}
		if listW+previewW+dividerWidth != inner {
			t.Fatalf("Split(%d) = (%d, %d): columns plus divider != %d", inner, listW, previewW, inner)
		}
		if listW < previewW {
			t.Fatalf("Split(%d) = (%d, %d): the list must keep the larger share", inner, listW, previewW)
		}
	}
	// A wide box spends the extra cells on the list, not the excerpt.
	if _, previewW := Split(400); previewW != MaxColumnWidth {
		t.Fatalf("wide preview width = %d, want the %d cap", previewW, MaxColumnWidth)
	}
}

// TestColumnsAlignsRuleAndPads is the layout criterion: the rule sits in one
// column on every row, the block is exactly height rows however short the
// list is, and the excerpt lands right of the rule.
func TestColumnsAlignsRuleAndPads(t *testing.T) {
	path := writeLines(t, 40)
	var c Cache
	const inner, height = 100, 11
	listW, previewW := Split(inner)
	rows := strings.Split(c.Columns([]string{"first row", "second row"}, listW, previewW, height, Target{Path: path, Line: 20}, nil), "\n")
	if len(rows) != height {
		t.Fatalf("got %d rows, want %d", len(rows), height)
	}
	for i, r := range rows {
		plainRow := ansi.Strip(r)
		if w := ansi.StringWidth(r); w > inner {
			t.Fatalf("row %d width %d > %d", i, w, inner)
		}
		if got := []rune(plainRow)[listW+1]; got != '│' {
			t.Fatalf("row %d: rule at column %d is %q", i, listW+1, got)
		}
	}
	right := ansi.Strip(rows[height/2])
	if !strings.Contains(right, "line 20") {
		t.Fatalf("center row = %q, want the target line", right)
	}
	if !strings.HasPrefix(ansi.Strip(rows[0]), "first row") {
		t.Fatalf("row 0 = %q, want the list row first", ansi.Strip(rows[0]))
	}
}

// TestColumnsWithoutPreviewReturnsList: a zero preview width (a box too narrow
// to split) yields the padded list alone, so callers need no branch of their
// own.
func TestColumnsWithoutPreviewReturnsList(t *testing.T) {
	var c Cache
	rows := strings.Split(c.Columns([]string{"only"}, 40, 0, 4, Target{Path: "whatever", Line: 3}, nil), "\n")
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}
	if rows[0] != "only" || rows[1] != "" || rows[3] != "" {
		t.Fatalf("rows = %q, want the list blank-padded", rows)
	}
}

// TestColumnsUnreadableTargetDegrades keeps the acceptance criterion that a
// deleted or pathless target never breaks the frame.
func TestColumnsUnreadableTargetDegrades(t *testing.T) {
	var c Cache
	listW, previewW := Split(100)
	body := c.Columns([]string{"row"}, listW, previewW, 6, Target{Path: "/no/such/file.go", Line: 4}, nil)
	if !strings.Contains(ansi.Strip(body), Unavailable) {
		t.Fatalf("missing file: body = %q, want the %q notice", body, Unavailable)
	}
	blank := c.Columns([]string{"row"}, listW, previewW, 6, Target{}, nil)
	if strings.Contains(ansi.Strip(blank), Unavailable) {
		t.Fatal("a row without a target must render a blank column, not a notice")
	}
	if got := len(strings.Split(blank, "\n")); got != 6 {
		t.Fatalf("got %d rows, want 6", got)
	}
}

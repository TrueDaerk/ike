package gridview

import (
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestTruncRightKeepsPrefix(t *testing.T) {
	cases := []struct {
		s     string
		width int
		want  string
	}{
		{"SomeLongClassName::someVeryLongMethodName", 30, "SomeLongClassName::someVeryLo…"},
		{"short", 10, "short"},
		{"exact", 5, "exact"},
		{"abcdef", 1, "…"},
		{"abcdef", 0, ""},
		{"abcdef", -3, ""},
		{"größenänderung", 6, "größe…"},
		// A wide rune that does not fit whole is dropped, not split.
		{"日本語の関数", 6, "日本…"},
		{"日本語の関数", 7, "日本語…"},
	}
	for _, c := range cases {
		got := TruncRight(c.s, c.width)
		if got != c.want {
			t.Errorf("TruncRight(%q, %d) = %q, want %q", c.s, c.width, got, c.want)
		}
		if c.width >= 0 && lipgloss.Width(got) > c.width {
			t.Errorf("TruncRight(%q, %d) is %d cells wide", c.s, c.width, lipgloss.Width(got))
		}
	}
}

func TestTruncLeftKeepsFileName(t *testing.T) {
	cases := []struct {
		s     string
		width int
		want  string
	}{
		{"internal/complexitypanel/complexitypanel.go", 36, "…/complexitypanel/complexitypanel.go"},
		{"a/b.go", 6, "a/b.go"},
		{"abcdef", 1, "…"},
		{"abcdef", 0, ""},
		{"ünïcödé/fïlé.go", 8, "…fïlé.go"},
		{"パス/ファイル.go", 8, "…イル.go"},
	}
	for _, c := range cases {
		got := TruncLeft(c.s, c.width)
		if got != c.want {
			t.Errorf("TruncLeft(%q, %d) = %q, want %q", c.s, c.width, got, c.want)
		}
		if lipgloss.Width(got) > max(c.width, 0) {
			t.Errorf("TruncLeft(%q, %d) is %d cells wide", c.s, c.width, lipgloss.Width(got))
		}
	}
}

func TestTruncMarkedKeepsMarks(t *testing.T) {
	if got, want := TruncMarked("▸ ", "veryLongFunctionName", " ✓ ★", 16, false), "▸ veryLongF… ✓ ★"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := TruncMarked("", "dir/sub/file.go", " ▲", 10, true), "…file.go ▲"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := TruncMarked("▸ ", "fits", "", 10, false); got != "▸ fits" {
		t.Fatalf("a fitting string changed: %q", got)
	}
	// No room left for the core: a plain right cut.
	if got, want := TruncMarked("▸ ", "name", " ✓", 4, false), "▸ n…"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFitWidthsKeepsFittingColumns(t *testing.T) {
	nat := []int{20, 30, 4, 3}
	text := []bool{true, true, false, false}
	if got := FitWidths(nat, text, 50, MinTextWidth); !reflect.DeepEqual(got, nat) {
		t.Fatalf("fitting widths changed: %v", got)
	}
}

func TestFitWidthsSplitsProportionally(t *testing.T) {
	nat := []int{80, 60, 4, 3}
	text := []bool{true, true, false, false}
	got := FitWidths(nat, text, 70, MinTextWidth)
	if got[0]+got[1] != 70 || got[2] != 4 || got[3] != 3 {
		t.Fatalf("widths = %v, want text sum 70 and numeric unchanged", got)
	}
	if got[0] != 40 || got[1] != 30 {
		t.Fatalf("widths = %v, want the 4:3 split 40/30", got)
	}
}

func TestFitWidthsHonoursMinimum(t *testing.T) {
	// The short column's share (100*14/214 = 6) is below the minimum: it is
	// pinned at 12, the long one takes the rest.
	got := FitWidths([]int{14, 200}, []bool{true, true}, 100, MinTextWidth)
	if want := []int{12, 88}; !reflect.DeepEqual(got, want) {
		t.Fatalf("widths = %v, want %v", got, want)
	}
	// A column narrower than the minimum keeps its natural width.
	got = FitWidths([]int{5, 200}, []bool{true, true}, 50, MinTextWidth)
	if want := []int{5, 45}; !reflect.DeepEqual(got, want) {
		t.Fatalf("widths = %v, want %v", got, want)
	}
}

func TestFitWidthsBelowMinimaSqueezesToMinimum(t *testing.T) {
	got := FitWidths([]int{40, 50, 8, 3}, []bool{true, true, true, false}, 20, MinTextWidth)
	if want := []int{12, 12, 8, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("widths = %v, want %v", got, want)
	}
}

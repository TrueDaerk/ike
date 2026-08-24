package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestClampResultRows pins the shared popup bounds (#2047).
func TestClampResultRows(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{-5, MinResultRows}, {0, MinResultRows}, {10, MinResultRows},
		{11, 11}, {25, 25}, {40, 40}, {41, MaxResultRows}, {500, MaxResultRows},
	} {
		if got := ClampResultRows(tc.in); got != tc.want {
			t.Errorf("ClampResultRows(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestPadRows keeps a block at an exact height in both directions.
func TestPadRows(t *testing.T) {
	if got := PadRows([]string{"a"}, 3); len(got) != 3 || got[1] != "" || got[2] != "" {
		t.Fatalf("PadRows short = %#v", got)
	}
	if got := PadRows([]string{"a", "b", "c"}, 2); len(got) != 2 {
		t.Fatalf("PadRows long = %#v", got)
	}
	if got := PadRows(nil, 0); len(got) != 0 {
		t.Fatalf("PadRows zero = %#v", got)
	}
}

// TestJoinColumns checks the rule lands in the same column on every row, so
// the divider reads as one unbroken vertical line.
func TestJoinColumns(t *testing.T) {
	out := JoinColumns([]string{"ab", "longer"}, 8, "│", []string{"x", "y", "z"})
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (the taller block wins)", len(lines))
	}
	for i, ln := range lines {
		if at := strings.Index(ansi.Strip(ln), "│"); at != 9 {
			t.Fatalf("line %d has the rule at %d, want 9: %q", i, at, ln)
		}
	}
	if !strings.HasSuffix(lines[2], "z") {
		t.Fatalf("line 2 = %q, want the right block's third row", lines[2])
	}
	if JoinColumns(nil, 4, "│", nil) != "" {
		t.Fatal("two empty blocks should render nothing")
	}
}

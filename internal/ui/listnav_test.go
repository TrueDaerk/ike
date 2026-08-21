package ui

import "testing"

func TestClampIndex(t *testing.T) {
	cases := []struct{ i, n, want int }{
		{-5, 3, 0}, {0, 3, 0}, {2, 3, 2}, {7, 3, 2}, {0, 0, 0}, {4, 0, 0}, {1, -2, 0},
	}
	for _, c := range cases {
		if got := ClampIndex(c.i, c.n); got != c.want {
			t.Errorf("ClampIndex(%d,%d) = %d, want %d", c.i, c.n, got, c.want)
		}
	}
}

func TestStepIndexWrapsAtBothEnds(t *testing.T) {
	cases := []struct{ i, delta, n, want int }{
		{0, 1, 5, 1},
		{4, 1, 5, 0},  // down off the last entry wraps to the first
		{0, -1, 5, 4}, // up off the first wraps to the last
		{2, -1, 5, 1},
		{0, 1, 1, 0},  // single entry: every step stays put
		{0, -1, 1, 0}, //
		{0, 1, 0, 0},  // empty list
		{0, -1, 0, 0},
		{0, 7, 5, 2}, // multi-row deltas wrap too (wheel scrolling)
		{0, -7, 5, 3},
	}
	for _, c := range cases {
		if got := StepIndex(c.i, c.delta, c.n); got != c.want {
			t.Errorf("StepIndex(%d,%d,%d) = %d, want %d", c.i, c.delta, c.n, got, c.want)
		}
	}
}

func TestPageIndexClampsAtEnds(t *testing.T) {
	cases := []struct{ i, delta, n, page, want int }{
		{0, 1, 100, 10, 10},
		{95, 1, 100, 10, 99}, // clamps, does not wrap
		{3, -1, 100, 10, 0},  // clamps, does not wrap
		{50, -1, 100, 10, 40},
		{0, 1, 5, 0, 1},   // page < 1 degrades to a single row
		{0, 1, 0, 10, 0},  // empty list
		{0, 1, 1, 10, 0},  // single entry
		{0, -1, 1, 10, 0}, //
	}
	for _, c := range cases {
		if got := PageIndex(c.i, c.delta, c.n, c.page); got != c.want {
			t.Errorf("PageIndex(%d,%d,%d,%d) = %d, want %d", c.i, c.delta, c.n, c.page, got, c.want)
		}
	}
}

func TestScrollToShow(t *testing.T) {
	cases := []struct{ top, sel, height, n, want int }{
		{0, 0, 10, 100, 0},
		{0, 5, 10, 100, 0},    // already visible
		{0, 10, 10, 100, 1},   // one past the window bottom
		{20, 5, 10, 100, 5},   // above the window
		{95, 99, 10, 100, 90}, // never scrolls past the end
		{0, 0, 0, 100, 0},     // unsized viewport
		{0, 2, 10, 3, 0},      // content shorter than the window
	}
	for _, c := range cases {
		if got := ScrollToShow(c.top, c.sel, c.height, c.n); got != c.want {
			t.Errorf("ScrollToShow(%d,%d,%d,%d) = %d, want %d", c.top, c.sel, c.height, c.n, got, c.want)
		}
	}
}

// TestScrollToShowOff covers the scrolloff margin (#2041): the window moves on
// one row early, but never past a list end and never beyond half the window.
func TestScrollToShowOff(t *testing.T) {
	cases := []struct{ top, sel, height, n, off, want int }{
		{0, 3, 5, 10, 1, 0},    // row 4 still visible below the cursor
		{0, 4, 5, 10, 1, 1},    // the margin pulls the window on early
		{1, 2, 5, 10, 1, 1},    // row 1 still visible above the cursor
		{2, 2, 5, 10, 1, 1},    // one row above the cursor scrolls back
		{5, 9, 5, 10, 1, 5},    // the last row sits flush at the window edge
		{0, 0, 5, 10, 1, 0},    // …as does the first
		{0, 2, 5, 3, 1, 0},     // list shorter than the window: no scroll
		{0, 4, 5, 10, 9, 2},    // the margin is capped at (height-1)/2 = 2
		{0, 4, 5, 10, 0, 0},    // off=0 is the plain edge-triggered window
		{0, 4, 5, 10, -1, 0},   // a negative margin is treated as none
		{20, 50, 0, 100, 1, 0}, // unsized viewport
	}
	for _, c := range cases {
		if got := ScrollToShowOff(c.top, c.sel, c.height, c.n, c.off); got != c.want {
			t.Errorf("ScrollToShowOff(%d,%d,%d,%d,%d) = %d, want %d",
				c.top, c.sel, c.height, c.n, c.off, got, c.want)
		}
	}
}

func TestListNavWrapsStepsAndClampsPages(t *testing.T) {
	sel := 0
	if !ListNav("up", &sel, 5, 3, NavDefault) || sel != 4 {
		t.Fatalf("up on the first entry: sel = %d, want 4", sel)
	}
	if !ListNav("down", &sel, 5, 3, NavDefault) || sel != 0 {
		t.Fatalf("down on the last entry: sel = %d, want 0", sel)
	}
	if !ListNav("ctrl+n", &sel, 5, 3, NavDefault) || sel != 1 {
		t.Fatalf("ctrl+n: sel = %d, want 1", sel)
	}
	if !ListNav("ctrl+p", &sel, 5, 3, NavDefault) || sel != 0 {
		t.Fatalf("ctrl+p: sel = %d, want 0", sel)
	}
	if !ListNav("pgup", &sel, 5, 3, NavDefault) || sel != 0 {
		t.Fatalf("pgup on the first entry must clamp: sel = %d, want 0", sel)
	}
	if !ListNav("pgdown", &sel, 5, 3, NavDefault) || sel != 3 {
		t.Fatalf("pgdown: sel = %d, want 3", sel)
	}
	if !ListNav("pgdown", &sel, 5, 3, NavDefault) || sel != 4 {
		t.Fatalf("pgdown on the last page must clamp: sel = %d, want 4", sel)
	}
	if !ListNav("home", &sel, 5, 3, NavDefault) || sel != 0 {
		t.Fatalf("home: sel = %d, want 0", sel)
	}
	if !ListNav("end", &sel, 5, 3, NavDefault) || sel != 4 {
		t.Fatalf("end: sel = %d, want 4", sel)
	}
}

func TestListNavKeySets(t *testing.T) {
	sel := 0
	if ListNav("j", &sel, 5, 3, NavDefault) {
		t.Error("NavDefault must not claim j")
	}
	if ListNav("home", &sel, 5, 3, NavArrows) {
		t.Error("NavArrows must not claim home")
	}
	if ListNav("ctrl+n", &sel, 5, 3, NavArrows) {
		t.Error("NavArrows must not claim ctrl+n")
	}
	if !ListNav("j", &sel, 5, 3, NavFull) || sel != 1 {
		t.Errorf("NavFull j: sel = %d, want 1", sel)
	}
	if !ListNav("G", &sel, 5, 3, NavFull) || sel != 4 {
		t.Errorf("NavFull G: sel = %d, want 4", sel)
	}
	if !ListNav("g", &sel, 5, 3, NavFull) || sel != 0 {
		t.Errorf("NavFull g: sel = %d, want 0", sel)
	}
	if ListNav("x", &sel, 5, 3, NavFull) {
		t.Error("unknown key must not be consumed")
	}
}

func TestListNavEmptyListConsumesNothing(t *testing.T) {
	sel := 0
	for _, k := range []string{"up", "down", "pgup", "pgdown", "home", "end"} {
		if ListNav(k, &sel, 0, 5, NavFull) {
			t.Errorf("%q must not be consumed on an empty list", k)
		}
	}
	if sel != 0 {
		t.Errorf("sel = %d, want 0", sel)
	}
}

func TestListNavSingleEntry(t *testing.T) {
	sel := 0
	for _, k := range []string{"up", "down", "pgup", "pgdown", "home", "end"} {
		if !ListNav(k, &sel, 1, 5, NavFull) {
			t.Errorf("%q must be consumed on a one-entry list", k)
		}
		if sel != 0 {
			t.Errorf("after %q: sel = %d, want 0", k, sel)
		}
	}
}

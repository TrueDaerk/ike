package ui

import (
	"testing"
	"time"
)

func TestWheelWindowClampsAtLastPage(t *testing.T) {
	// 10 rows in a 4-row window: the window may travel to top 6 and no
	// further, so the last page always renders full.
	top, cur := 0, 0
	WheelWindow(&top, &cur, 3, 10, 4)
	if top != 3 || cur != 3 {
		t.Fatalf("top, cursor = %d, %d, want 3, 3", top, cur)
	}
	WheelWindow(&top, &cur, 100, 10, 4)
	if top != 6 {
		t.Fatalf("top = %d, want clamped to 6", top)
	}
	if cur < top || cur > top+3 {
		t.Fatalf("cursor %d left the window [%d, %d]", cur, top, top+3)
	}
}

func TestWheelWindowClampsAtTop(t *testing.T) {
	top, cur := 5, 7
	WheelWindow(&top, &cur, -100, 10, 4)
	if top != 0 {
		t.Fatalf("top = %d, want 0", top)
	}
	if cur != 3 {
		t.Fatalf("cursor = %d, want dragged to the window's last row 3", cur)
	}
}

func TestWheelWindowShortListNeverScrolls(t *testing.T) {
	// Fewer rows than the window: there is nothing to scroll, so a flick in
	// either direction is inert.
	top, cur := 0, 1
	WheelWindow(&top, &cur, 5, 3, 10)
	if top != 0 || cur != 1 {
		t.Fatalf("top, cursor = %d, %d, want 0, 1", top, cur)
	}
}

func TestWheelWindowEmptyList(t *testing.T) {
	top, cur := 4, 4
	WheelWindow(&top, &cur, 2, 0, 5)
	if top != 0 || cur != 0 {
		t.Fatalf("top, cursor = %d, %d, want both reset to 0", top, cur)
	}
}

func TestWheelWindowDegenerateHeight(t *testing.T) {
	// A pane rendered into no rows yet still scrolls one row per notch
	// instead of dividing by zero or scrolling to the end.
	top, cur := 0, 0
	WheelWindow(&top, &cur, 1, 10, 0)
	if top != 1 || cur != 1 {
		t.Fatalf("top, cursor = %d, %d, want 1, 1", top, cur)
	}
}

func TestRowAtHitsBodyRows(t *testing.T) {
	// One header line, a 3-row body, 10 rows, scrolled to top 4.
	if i, ok := RowAt(1, 4, 1, 3, 10); !ok || i != 4 {
		t.Fatalf("RowAt(1) = %d, %v, want 4, true", i, ok)
	}
	if i, ok := RowAt(3, 4, 1, 3, 10); !ok || i != 6 {
		t.Fatalf("RowAt(3) = %d, %v, want 6, true", i, ok)
	}
}

func TestRowAtRejectsChrome(t *testing.T) {
	if _, ok := RowAt(0, 0, 1, 3, 10); ok {
		t.Fatal("header line must not hit a row")
	}
	if _, ok := RowAt(4, 0, 1, 3, 10); ok {
		t.Fatal("the line below the body (footer) must not hit a row")
	}
	if _, ok := RowAt(-1, 0, 1, 3, 10); ok {
		t.Fatal("a negative y must not hit a row")
	}
}

func TestRowAtRejectsBlankTail(t *testing.T) {
	// Two rows in a 5-row body: the three blank rows below them are not
	// clickable.
	if i, ok := RowAt(2, 0, 1, 5, 2); !ok || i != 1 {
		t.Fatalf("RowAt(2) = %d, %v, want 1, true", i, ok)
	}
	if _, ok := RowAt(3, 0, 1, 5, 2); ok {
		t.Fatal("a blank row below the list must not hit a row")
	}
	if _, ok := RowAt(1, 0, 1, 5, 0); ok {
		t.Fatal("an empty list has no clickable rows")
	}
}

func TestRowAtHeaderlessBody(t *testing.T) {
	if i, ok := RowAt(0, 2, 0, 4, 10); !ok || i != 2 {
		t.Fatalf("RowAt(0) = %d, %v, want 2, true", i, ok)
	}
}

func TestClickTrackerDoubleOnSameRow(t *testing.T) {
	var tr ClickTracker
	at := time.Unix(0, 0)
	if tr.Double(3, at) {
		t.Fatal("the first click on a row is single")
	}
	if !tr.Double(3, at.Add(100*time.Millisecond)) {
		t.Fatal("a prompt second click on the same row is a double click")
	}
}

func TestClickTrackerRejectsSlowAndOtherRow(t *testing.T) {
	var tr ClickTracker
	at := time.Unix(0, 0)
	tr.Double(3, at)
	if tr.Double(3, at.Add(DoubleClickWindow+time.Millisecond)) {
		t.Fatal("a click past the window is a fresh single click")
	}
	tr.Double(3, at)
	if tr.Double(4, at.Add(time.Millisecond)) {
		t.Fatal("a prompt click on another row is single")
	}
}

func TestClickTrackerZeroRowIsSingle(t *testing.T) {
	// The zero value must not read as "row 0 was just clicked at the zero
	// time" — that is only harmless because the elapsed time is huge, and a
	// clock injected from time.Unix(0, 0) would make it a false double.
	var tr ClickTracker
	if tr.Double(0, time.Unix(0, 0)) {
		t.Fatal("the first click on row 0 must be single even at the zero time")
	}
}

func TestClickTrackerReset(t *testing.T) {
	var tr ClickTracker
	at := time.Unix(0, 0)
	tr.Double(3, at)
	tr.Reset()
	if tr.Double(3, at.Add(time.Millisecond)) {
		t.Fatal("a reset tracker must not complete a double click")
	}
}

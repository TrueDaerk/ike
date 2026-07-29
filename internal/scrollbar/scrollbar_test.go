package scrollbar

import "testing"

func TestThumbClamps(t *testing.T) {
	if s, l := Thumb(10, 5, 10, 0); s != 0 || l != 10 {
		t.Fatalf("no overflow should fill the track, got start=%d len=%d", s, l)
	}
	if _, l := Thumb(10, 1000, 10, 0); l != 1 {
		t.Fatalf("huge content should clamp the thumb to 1, got %d", l)
	}
	if s, l := Thumb(10, 1000, 10, 990); s+l != 10 {
		t.Fatalf("bottom offset should park the thumb at the end, got start=%d len=%d", s, l)
	}
	if s, l := Thumb(0, 100, 10, 0); s != 0 || l != 0 {
		t.Fatalf("zero track should yield nothing, got start=%d len=%d", s, l)
	}
}

func TestThumbTracksOffset(t *testing.T) {
	// 20 lines in a 10-row window on a 10-row track: thumb is half the track
	// and its start scales with the offset.
	if s, l := Thumb(10, 20, 10, 0); s != 0 || l != 5 {
		t.Fatalf("top: got start=%d len=%d", s, l)
	}
	if s, _ := Thumb(10, 20, 10, 10); s != 5 {
		t.Fatalf("bottom: got start=%d", s)
	}
	if s, _ := Thumb(10, 20, 10, 5); s != 2 {
		t.Fatalf("middle: got start=%d", s)
	}
}

func TestJump(t *testing.T) {
	// 100 lines, 10 visible: pressing the track ends maps to the offset ends.
	if got := Jump(0, 10, 100, 10, 42); got != 0 {
		t.Fatalf("top press should jump to 0, got %d", got)
	}
	if got := Jump(9, 10, 100, 10, 0); got != 90 {
		t.Fatalf("bottom press should jump to maxOff, got %d", got)
	}
	if got := Jump(5, 10, 100, 10, 0); got != 50 {
		t.Fatalf("middle press should jump proportionally, got %d", got)
	}
	// No overflow: the offset stays put.
	if got := Jump(5, 10, 8, 10, 3); got != 3 {
		t.Fatalf("no overflow should keep the offset, got %d", got)
	}
}

func TestDrag(t *testing.T) {
	// 20 lines, 10 visible, thumb length 5: dragging the thumb's top across
	// the 5 free rows sweeps the full offset range.
	if got := Drag(0, 0, 10, 20, 10, 5); got != 0 {
		t.Fatalf("drag to top should reach 0, got %d", got)
	}
	if got := Drag(9, 0, 10, 20, 10, 5); got != 10 {
		t.Fatalf("drag past the end should clamp to maxOff, got %d", got)
	}
	// The grab offset anchors the pointer inside the thumb.
	if got := Drag(3, 3, 10, 20, 10, 0); got != 0 {
		t.Fatalf("grabbed mid-thumb, no motion: offset should stay 0, got %d", got)
	}
	// A thumb filling the track cannot drag.
	if got := Drag(5, 0, 10, 10, 10, 0); got != 0 {
		t.Fatalf("full thumb should keep the offset, got %d", got)
	}
}

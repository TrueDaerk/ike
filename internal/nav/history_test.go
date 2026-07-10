package nav

import "testing"

func pos(path string, line int) Position { return Position{Path: path, Line: line} }

func TestBackForwardRoundTrip(t *testing.T) {
	var h History
	if h.CanBack() || h.CanForward() {
		t.Fatal("zero history must be empty")
	}
	if _, ok := h.Back(pos("a.go", 0)); ok {
		t.Fatal("empty back must report false")
	}

	// a:10 -> b:20 -> c:30 records the two departure points.
	h.RecordJump(pos("a.go", 10))
	h.RecordJump(pos("b.go", 20))

	back, ok := h.Back(pos("c.go", 30))
	if !ok || back != pos("b.go", 20) {
		t.Fatalf("back = %+v ok=%v", back, ok)
	}
	back, ok = h.Back(pos("b.go", 20))
	if !ok || back != pos("a.go", 10) {
		t.Fatalf("back2 = %+v ok=%v", back, ok)
	}
	if h.CanBack() {
		t.Fatal("back stack must be drained")
	}

	// Forward re-traverses the same chain.
	fwd, ok := h.Forward(pos("a.go", 10))
	if !ok || fwd != pos("b.go", 20) {
		t.Fatalf("forward = %+v ok=%v", fwd, ok)
	}
	fwd, ok = h.Forward(pos("b.go", 20))
	if !ok || fwd != pos("c.go", 30) {
		t.Fatalf("forward2 = %+v ok=%v", fwd, ok)
	}
	if h.CanForward() {
		t.Fatal("forward stack must be drained")
	}
	// And back works again after the round trip.
	if back, ok := h.Back(pos("c.go", 30)); !ok || back != pos("b.go", 20) {
		t.Fatalf("back after forward = %+v ok=%v", back, ok)
	}
}

func TestFreshJumpTruncatesForward(t *testing.T) {
	var h History
	h.RecordJump(pos("a.go", 1))
	h.RecordJump(pos("b.go", 2))
	if _, ok := h.Back(pos("c.go", 3)); !ok {
		t.Fatal("back failed")
	}
	if !h.CanForward() {
		t.Fatal("forward must hold the departed position")
	}
	// A fresh jump while back in history drops the forward tail.
	h.RecordJump(pos("d.go", 4))
	if h.CanForward() {
		t.Fatal("fresh jump must truncate forward")
	}
}

func TestDedupNearPositions(t *testing.T) {
	var h History
	h.RecordJump(Position{Path: "a.go", Line: 5, Col: 1})
	h.RecordJump(Position{Path: "a.go", Line: 5, Col: 30}) // same spot, column drift
	h.RecordJump(pos("b.go", 7))
	back, ok := h.Back(pos("c.go", 9))
	if !ok || back != pos("b.go", 7) {
		t.Fatalf("back = %+v", back)
	}
	back, ok = h.Back(back)
	if !ok || back.Path != "a.go" || back.Line != 5 || back.Col != 30 {
		t.Fatalf("collapsed entry must keep the freshest column: %+v", back)
	}
	if h.CanBack() {
		t.Fatal("the two near positions must have collapsed into one entry")
	}
}

func TestBackSkipsSelfEntry(t *testing.T) {
	var h History
	h.RecordJump(pos("a.go", 1))
	h.RecordJump(pos("b.go", 2))
	// Caret sits exactly on the newest entry: back must not "jump" in place.
	back, ok := h.Back(pos("b.go", 2))
	if !ok || back != pos("a.go", 1) {
		t.Fatalf("back = %+v ok=%v", back, ok)
	}
}

func TestPathlessPositionsIgnored(t *testing.T) {
	var h History
	h.RecordJump(Position{})
	if h.CanBack() {
		t.Fatal("pathless jump must not record")
	}
	h.RecordJump(pos("a.go", 1))
	if back, ok := h.Back(Position{}); !ok || back != pos("a.go", 1) {
		t.Fatalf("back = %+v ok=%v", back, ok)
	}
	// The pathless current position must not have landed on forward.
	if h.CanForward() {
		t.Fatal("pathless current must not be pushed forward")
	}
}

func TestCapDropsOldest(t *testing.T) {
	var h History
	for i := 0; i < maxEntries+50; i++ {
		h.RecordJump(pos("f.go", i))
	}
	if len(h.back) != maxEntries {
		t.Fatalf("back len = %d want %d", len(h.back), maxEntries)
	}
	if h.back[0].Line != 50 {
		t.Fatalf("oldest kept = %+v, the first 50 should have fallen off", h.back[0])
	}
}

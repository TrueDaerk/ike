package tracepanel

import "testing"

// snake_room_test.go covers #2909: after a turn the row runs in the
// direction that fits the whole turn (the stops up to the next break), else
// the one with more room; a tie keeps the snake reversing.

// sameRowFrom asserts that slots from..to-1 sit side by side on the row of
// slot from, running in direction dir.
func sameRowFrom(t *testing.T, l Layout, from, to, dir int) {
	t.Helper()
	first := l.Slots[from]
	step := l.BoxW + gapW
	for i := from; i < to; i++ {
		s := l.Slots[i]
		if s.Row != first.Row || s.Dir != dir || s.X != first.X+dir*(i-from)*step {
			t.Fatalf("slot %d = %+v, want row %d dir %d beside %+v", i, s, first.Row, dir, first)
		}
	}
}

func TestSnakeTurnRunsTowardsMoreRoom(t *testing.T) {
	// 97 cells hold four 22-cell boxes per row.
	const w = 97
	bw, _ := BoxWidth(w)
	if bw*4+3*gapW != w {
		t.Fatalf("box width %d does not fill %d cells with four boxes", bw, w)
	}
	step := bw + gapW
	// Turn 1: four boxes rightwards, two leftwards — row 1 ends at column
	// 3 of 4 (direction -1). Turn 2: prompt + change + answer. Reversed
	// (rightwards) has room for one box, leftwards for the whole turn.
	l := Snake(widthsFor(9, -1, w), []int{6}, w, -1, 0)
	if last := l.Slots[5]; last.Row != 1 || last.X != 2*step || last.Dir != -1 {
		t.Fatalf("turn 1 must end at column 3 going left, got %+v", last)
	}
	if p := l.Slots[6]; !p.Break || p.Row != 2 || p.X != 2*step {
		t.Fatalf("prompt #2 must sit under the last box, got %+v", p)
	}
	sameRowFrom(t, l, 6, 9, -1)
	// Mirror: row 2 ends at column 2 going right (direction +1); the
	// reversed direction has room for one box, rightwards for the turn.
	l = Snake(widthsFor(13, -1, w), []int{10}, w, -1, 0)
	if last := l.Slots[9]; last.Row != 2 || last.X != step || last.Dir != 1 {
		t.Fatalf("turn 1 must end at column 2 going right, got %+v", last)
	}
	sameRowFrom(t, l, 10, 13, 1)
	// #2901: the reversed direction has no room at all.
	l = Snake(widthsFor(11, -1, w), []int{9}, w, -1, 0)
	if last := l.Slots[8]; last.Row != 2 || last.X != 0 {
		t.Fatalf("turn 1 must end alone at the left edge, got %+v", last)
	}
	sameRowFrom(t, l, 9, 11, 1)
	// A turn longer than a row fits neither way: it takes the direction
	// with more room (leftwards: two boxes against one).
	l = Snake(widthsFor(12, -1, w), []int{6}, w, -1, 0)
	sameRowFrom(t, l, 6, 9, -1)
	if s := l.Slots[9]; s.Row != 3 {
		t.Fatalf("the fourth stop of turn 2 must turn, got %+v", s)
	}
}

func TestSnakeTurnTieKeepsReversing(t *testing.T) {
	// 122 cells hold five 22-cell boxes per row.
	const w = 122
	bw, _ := BoxWidth(w)
	if bw*5+4*gapW != w {
		t.Fatalf("box width %d does not fill %d cells with five boxes", bw, w)
	}
	// Turn 1 ends on row 1 at the middle column going left: two boxes of
	// room either way. A three-stop turn fits both ways, a four-stop one
	// neither; both reverse.
	for _, n := range []int{11, 12} {
		l := Snake(widthsFor(n, -1, w), []int{8}, w, -1, 0)
		if last := l.Slots[7]; last.Row != 1 || last.X != 2*(bw+gapW) || last.Dir != -1 {
			t.Fatalf("turn 1 must end mid-row going left, got %+v", last)
		}
		if p := l.Slots[8]; p.Row != 2 || p.Dir != 1 {
			t.Fatalf("%d-stop turn: prompt #2 = %+v, want it reversing rightwards", n-8, p)
		}
	}
}

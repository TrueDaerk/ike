package ui

import "testing"

// matchstep_test.go covers #2410's shared half: the stepping primitives every
// pane's NextMatch/PrevMatch is built from, and the counter they all render.

func TestStepWrapWrapsAtBothEnds(t *testing.T) {
	cases := []struct {
		name        string
		cur, n, del int
		want        int
		wrapped     bool
	}{
		{"forward", 0, 3, 1, 1, false},
		{"backward", 2, 3, -1, 1, false},
		{"off the end wraps", 2, 3, 1, 0, true},
		{"off the start wraps", 0, 3, -1, 2, true},
		{"no current goes first", -1, 3, 1, 0, false},
		{"no current backwards goes last", -1, 3, -1, 2, false},
		{"single match stays put but wraps", 0, 1, 1, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, wrapped := StepWrap(tc.cur, tc.n, tc.del)
			if got != tc.want || wrapped != tc.wrapped {
				t.Fatalf("StepWrap(%d,%d,%d) = %d,%v want %d,%v",
					tc.cur, tc.n, tc.del, got, wrapped, tc.want, tc.wrapped)
			}
		})
	}
}

func TestStepWrapOnAnEmptySet(t *testing.T) {
	if got, wrapped := StepWrap(0, 0, 1); got != -1 || wrapped {
		t.Fatalf("empty set = %d,%v want -1,false", got, wrapped)
	}
}

func TestStepSortedWalksPositions(t *testing.T) {
	list := []int{2, 5, 9}
	val, idx, wrapped, ok := StepSorted(list, 5, 1)
	if !ok || val != 9 || idx != 2 || wrapped {
		t.Fatalf("forward = %d,%d,%v,%v want 9,2,false,true", val, idx, wrapped, ok)
	}
	val, idx, wrapped, _ = StepSorted(list, 9, 1)
	if val != 2 || idx != 0 || !wrapped {
		t.Fatalf("forward off the end = %d,%d,%v want 2,0,true", val, idx, wrapped)
	}
	val, idx, wrapped, _ = StepSorted(list, 2, -1)
	if val != 9 || idx != 2 || !wrapped {
		t.Fatalf("backward off the start = %d,%d,%v want 9,2,true", val, idx, wrapped)
	}
	if _, _, _, ok := StepSorted(nil, 0, 1); ok {
		t.Fatal("an empty match list must report ok=false")
	}
}

// TestStepOverSkipsNonMatches: the list panes interleave group headers with
// results, and only the results are matches.
func TestStepOverSkipsNonMatches(t *testing.T) {
	// rows: header, hit, hit, header, hit
	hit := func(i int) bool { return i != 0 && i != 3 }
	next, st := StepOver(1, 5, 1, hit)
	if next != 2 || !st.Handled || st.Index != 2 || st.Total != 3 || st.Wrapped {
		t.Fatalf("forward = %d, %+v", next, st)
	}
	// From the last hit forward, the walk wraps back onto the first one —
	// never onto the header above it.
	next, st = StepOver(4, 5, 1, hit)
	if next != 1 || st.Index != 1 || !st.Wrapped {
		t.Fatalf("wrap = %d, %+v", next, st)
	}
	// A cursor parked on a header counts as the next hit below it.
	next, st = StepOver(3, 5, 1, hit)
	if next != 1 || st.Index != 1 || !st.Wrapped {
		t.Fatalf("from a header = %d, %+v", next, st)
	}
	if _, st := StepOver(0, 3, 1, func(int) bool { return false }); !st.Handled || st.Total != 0 {
		t.Fatalf("no matches = %+v want handled with total 0", st)
	}
}

func TestMatchCounterMarksTheWrap(t *testing.T) {
	if got := MatchCounter(3, 17, false); got != "3/17" {
		t.Fatalf("plain = %q", got)
	}
	if got := MatchCounter(1, 12, true); got != "1/12 (wrapped)" {
		t.Fatalf("wrapped = %q", got)
	}
	if got := MatchCounter(0, 0, false); got != "no matches" {
		t.Fatalf("empty = %q", got)
	}
}

func TestSteppedAndNoMatches(t *testing.T) {
	st := Stepped(2, 5, true)
	if !st.Handled || st.Index != 3 || st.Total != 5 || !st.Wrapped {
		t.Fatalf("Stepped = %+v", st)
	}
	if st := Stepped(0, 0, false); !st.Handled || st.Total != 0 {
		t.Fatalf("Stepped over nothing = %+v want the handled no-op", st)
	}
	if st := NoMatches(); !st.Handled || st.Total != 0 {
		t.Fatalf("NoMatches = %+v", st)
	}
	if NoStep.Handled {
		t.Fatal("NoStep must not be handled")
	}
}

// TestMatchStepChords pins the spellings the surfaces that own the keyboard
// answer themselves: cmd+g everywhere, ctrl+g as the non-mac secondary, and
// the JetBrains f3 pair.
func TestMatchStepChords(t *testing.T) {
	for _, k := range []string{"cmd+g", "super+g", "meta+g", "ctrl+g", "f3"} {
		if !NextMatchChord(k) {
			t.Errorf("%q must be a next-match chord", k)
		}
		if delta, ok := MatchStepChord(k); !ok || delta != 1 {
			t.Errorf("MatchStepChord(%q) = %d,%v want 1,true", k, delta, ok)
		}
	}
	// Both modifier orders: the keymap layer canonicalizes "cmd+shift+g",
	// bubbletea's own String() writes "shift+meta+g".
	for _, k := range []string{"cmd+shift+g", "shift+meta+g", "super+shift+g", "ctrl+shift+g", "shift+f3"} {
		if !PrevMatchChord(k) {
			t.Errorf("%q must be a prev-match chord", k)
		}
		if delta, ok := MatchStepChord(k); !ok || delta != -1 {
			t.Errorf("MatchStepChord(%q) = %d,%v want -1,true", k, delta, ok)
		}
	}
	for _, k := range []string{"g", "G", "cmd+f", "ctrl+n", "cmd+shift+f", "alt+g", "cmd+f3"} {
		if _, ok := MatchStepChord(k); ok {
			t.Errorf("%q must not be a match-step chord", k)
		}
	}
}

package editor

import "testing"

// nav_events_test.go pins the EventJump matrix (Roadmap 0220, #219): large
// motions and search landings emit the departure position; small motions
// never do.

// jumpEvents drives keys against a ten-line buffer and collects EventJump
// departures as (line, col) pairs.
func jumpEvents(t *testing.T, keys string) []Event {
	t.Helper()
	m, _ := loaded(t, "zero\none\ntwo\nthree match\nfour\nfive\nsix\nseven\neight\nnine match\n")
	var jumps []Event
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventJump {
			jumps = append(jumps, e)
		}
	}))
	typeKeys(m, keys)
	return jumps
}

func TestLargeMotionsEmitJump(t *testing.T) {
	cases := map[string]struct {
		keys     string
		wantLine int // departure line of the first jump
	}{
		"G":        {"G", 0},
		"gg after": {"Ggg", 9}, // G to the bottom, gg departs from line 9
		"countG":   {"5G", 0},
	}
	for label, c := range cases {
		jumps := jumpEvents(t, c.keys)
		if len(jumps) == 0 {
			t.Errorf("%s: no jump emitted", label)
			continue
		}
		last := jumps[len(jumps)-1]
		if label == "G" || label == "countG" {
			if jumps[0].Line != c.wantLine {
				t.Errorf("%s: departure line = %d want %d", label, jumps[0].Line, c.wantLine)
			}
		} else if last.Line != c.wantLine {
			t.Errorf("%s: departure line = %d want %d", label, last.Line, c.wantLine)
		}
	}
}

func TestSearchLandingsEmitJump(t *testing.T) {
	// Initial /-search: departs line 0 for the "match" on line 3.
	jumps := jumpEvents(t, "/match\r")
	if len(jumps) != 1 || jumps[0].Line != 0 {
		t.Fatalf("initial search jumps = %+v", jumps)
	}
	// n after a search departs the first landing (line 3) for line 9.
	jumps = jumpEvents(t, "/match\rn")
	if len(jumps) != 2 || jumps[1].Line != 3 {
		t.Fatalf("n jumps = %+v", jumps)
	}
	// * (search word under cursor) is a jump too.
	jumps = jumpEvents(t, "*")
	if len(jumps) != 1 || jumps[0].Line != 0 {
		t.Fatalf("* jumps = %+v", jumps)
	}
}

func TestSmallMotionsNeverEmitJump(t *testing.T) {
	for _, keys := range []string{"jjj", "wwb", "$0^", "}{", "llh", "5j"} {
		if jumps := jumpEvents(t, keys); len(jumps) != 0 {
			t.Errorf("%q: unexpected jump events %+v", keys, jumps)
		}
	}
}

func TestSetCursorEmitsCursorMove(t *testing.T) {
	// Programmatic placement (go-to-definition landing, usages pick, nav
	// back/forward) must emit a cursor-move so the LSP bridge tracks the new
	// position — otherwise rename/references right after a jump query the
	// pre-jump location (#371).
	m, path := loaded(t, "zero\none\ntwo\nthree\n")
	var moves []Event
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventCursorMove {
			moves = append(moves, e)
		}
	}))
	m.SetCursor(2, 2)
	if len(moves) != 1 {
		t.Fatalf("cursor-move events = %d, want 1 (%+v)", len(moves), moves)
	}
	if moves[0].Path != path || moves[0].Line != 2 || moves[0].Col != 2 {
		t.Fatalf("event = %s:%d:%d, want %s:2:2", moves[0].Path, moves[0].Line, moves[0].Col, path)
	}
	// Out-of-range coordinates emit the clamped landing, not the request.
	m.SetCursor(99, 99)
	last := moves[len(moves)-1]
	if last.Line != 3 {
		t.Fatalf("clamped event line = %d, want 3", last.Line)
	}
}

func TestOperatorWithLargeMotionDoesNotJump(t *testing.T) {
	// dG composes an operator over the motion: the caret does not travel in
	// the jump sense, so nothing is recorded.
	if jumps := jumpEvents(t, "dG"); len(jumps) != 0 {
		t.Fatalf("dG must not emit jumps: %+v", jumps)
	}
}

package terminal

import "testing"

// wheel_test.go covers the child-forwarding budget (#669): a coalesced wheel
// burst arrives as one large line delta, and only a bounded slice of it may
// be forwarded to the child — the backlog the coalescing removed must not
// reappear as a flood of PTY sequences.

func TestWheelChildBudget(t *testing.T) {
	cases := []struct {
		delta       int
		wantLines   int
		wantEvents  int
		description string
	}{
		{3, 3, 1, "single notch: one event, three lines"},
		{-3, 3, 1, "direction does not change the budget"},
		{9, 9, 3, "small burst passes through whole"},
		{-300, wheelChildCap, (wheelChildCap + wheelEventLines - 1) / wheelEventLines, "huge burst clamps to the cap"},
		{wheelChildCap + 1, wheelChildCap, (wheelChildCap + wheelEventLines - 1) / wheelEventLines, "cap boundary"},
		{4, 4, 2, "partial notch rounds the event count up"},
	}
	for _, c := range cases {
		lines, events := wheelChildBudget(c.delta)
		if lines != c.wantLines || events != c.wantEvents {
			t.Errorf("%s: wheelChildBudget(%d) = (%d, %d), want (%d, %d)",
				c.description, c.delta, lines, events, c.wantLines, c.wantEvents)
		}
	}
}

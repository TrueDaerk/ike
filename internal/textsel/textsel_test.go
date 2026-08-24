package textsel

import (
	"testing"
	"time"
)

// grid is a LineText over fixed lines.
func grid(lines ...string) LineText {
	return func(i int) []rune {
		if i < 0 || i >= len(lines) {
			return nil
		}
		return []rune(lines[i])
	}
}

func fixedClock(t *testing.T) func(time.Duration) {
	t.Helper()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	prev := Now
	Now = func() time.Time { return now }
	t.Cleanup(func() { Now = prev })
	return func(d time.Duration) { now = now.Add(d) }
}

func TestCharDragSelects(t *testing.T) {
	fixedClock(t)
	text := grid("alpha beta", "gamma")
	var s Selection
	s.Press(Pos{0, 6}, text)
	if s.Active() {
		t.Error("a bare press must not select yet")
	}
	s.Drag(Pos{1, 3}, text)
	s.Release()
	start, end, ok := s.Range()
	if !ok || start != (Pos{0, 6}) || end != (Pos{1, 3}) {
		t.Errorf("range: %v..%v ok=%v", start, end, ok)
	}
}

func TestDoubleClickWordAndUnitExtension(t *testing.T) {
	step := fixedClock(t)
	text := grid("alpha beta gamma")
	var s Selection
	s.Press(Pos{0, 7}, text)
	step(50 * time.Millisecond)
	s.Press(Pos{0, 7}, text)
	start, end, _ := s.Range()
	if start != (Pos{0, 6}) || end != (Pos{0, 10}) {
		t.Errorf("word span: %v..%v", start, end)
	}
	// Dragging backwards extends by whole words without eating the origin.
	s.Drag(Pos{0, 2}, text)
	start, end, _ = s.Range()
	if start != (Pos{0, 0}) || end != (Pos{0, 10}) {
		t.Errorf("word extension: %v..%v", start, end)
	}
}

func TestTripleClickLine(t *testing.T) {
	step := fixedClock(t)
	text := grid("alpha beta", "gamma")
	var s Selection
	for i := 0; i < 3; i++ {
		s.Press(Pos{0, 3}, text)
		step(50 * time.Millisecond)
	}
	start, end, _ := s.Range()
	if start != (Pos{0, 0}) || end != (Pos{0, 10}) {
		t.Errorf("line span: %v..%v", start, end)
	}
	s.Drag(Pos{1, 2}, text)
	_, end, _ = s.Range()
	if end != (Pos{1, 5}) {
		t.Errorf("line extension: end %v", end)
	}
}

func TestStreakExpires(t *testing.T) {
	step := fixedClock(t)
	text := grid("alpha")
	var s Selection
	s.Press(Pos{0, 2}, text)
	step(MultiClickWindow + time.Millisecond)
	s.Press(Pos{0, 2}, text)
	if s.Active() {
		t.Error("an expired streak must restart as a char press")
	}
}

func TestLineRange(t *testing.T) {
	fixedClock(t)
	text := grid("aaaa", "bbbb", "cccc")
	var s Selection
	s.Press(Pos{0, 2}, text)
	s.Drag(Pos{2, 1}, text)
	if a, b := s.LineRange(0, 4); a != 2 || b != 4 {
		t.Errorf("first line: [%d,%d)", a, b)
	}
	if a, b := s.LineRange(1, 4); a != 0 || b != 4 {
		t.Errorf("middle line: [%d,%d)", a, b)
	}
	if a, b := s.LineRange(2, 4); a != 0 || b != 1 {
		t.Errorf("last line: [%d,%d)", a, b)
	}
	if a, b := s.LineRange(3, 4); a != 0 || b != 0 {
		t.Errorf("uncovered line: [%d,%d)", a, b)
	}
}

func TestRawSliceMapsTabsBack(t *testing.T) {
	// "\tab" displays as four spaces then "ab" (tab width 4).
	if got := RawSlice("\tab", 0, 6, 4); got != "\tab" {
		t.Errorf("full cover: %q", got)
	}
	if got := RawSlice("\tab", 4, 5, 4); got != "a" {
		t.Errorf("past the tab: %q", got)
	}
	// A partially covered tab is included whole.
	if got := RawSlice("\tab", 2, 5, 4); got != "\ta" {
		t.Errorf("partial tab: %q", got)
	}
	if got := RawSlice("abc", 5, 9, 4); got != "" {
		t.Errorf("past the end: %q", got)
	}
	if got := RawSlice("abc", 2, 2, 4); got != "" {
		t.Errorf("empty interval: %q", got)
	}
}

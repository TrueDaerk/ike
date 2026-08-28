package merge

import (
	"strconv"
	"strings"
	"testing"
)

// longSide builds an n-line version of the fixture file.
func longSide(n int, mark string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(mark + strconv.Itoa(i) + "\n")
	}
	return b.String()
}

// TestWheelScrollsAllThreeColumns guards the wheel (#2259): it moves the
// result editor's viewport, and the side columns render off that same offset,
// so one gesture scrolls the whole view.
func TestWheelScrollsAllThreeColumns(t *testing.T) {
	m := newView(t)
	base := longSide(200, "l")
	m.SetContents(base, base, base)
	if top := m.ed.ScrollTop(); top != 0 {
		t.Fatalf("scroll top = %d, want 0 before the wheel", top)
	}
	m.Wheel(5)
	top := m.ed.ScrollTop()
	if top != 5 {
		t.Fatalf("scroll top = %d, want 5", top)
	}
	// The side columns are drawn from the same offset, so the first rendered
	// side line is the one at that row.
	if !strings.Contains(m.View(), "l5") {
		t.Fatalf("view does not show the scrolled side rows:\n%s", m.View())
	}
	m.Wheel(-100)
	if got := m.ed.ScrollTop(); got != 0 {
		t.Fatalf("scroll top = %d, want clamped back to 0", got)
	}
}

package diff

// selcache_test.go pins the selection rendering rule the drag latency fix
// established (#2495): the cached lines are selection-free and a drag never
// touches them, so the highlight is painted by View over the visible window.
// The semantics of #2070 (side pinning, fold copy, unified pairs) are covered
// by selection_test.go and must not move — these tests guard the caching that
// carries them.

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// scrollModel is a unified-layout diff several viewports tall, so a selection
// can reach past the window it was started in and lines can scroll into view
// while it lives.
func scrollModel(t *testing.T) *Model {
	t.Helper()
	var left, right strings.Builder
	for i := 0; i < 40; i++ {
		left.WriteString("line ")
		left.WriteString(string(rune('a' + i%26)))
		left.WriteByte('\n')
		right.WriteString("line ")
		right.WriteString(string(rune('a' + i%26)))
		right.WriteByte('\n')
	}
	m := testModel(t, left.String()+"tail\n", right.String()+"TAIL\n")
	m.SetContext(-1) // no folding: visual line i is row i
	m.SetUnified(true)
	return m
}

// TestDragLeavesTheLineCacheIntact is the fix's core invariant: a motion event
// during a drag moves the anchors and nothing else. The cached lines are not
// merely equal afterwards — they are the same backing array, so no re-render
// (and with it no re-diff and no re-highlight) can have happened.
func TestDragLeavesTheLineCacheIntact(t *testing.T) {
	m := scrollModel(t)
	fixedClock(t)
	if len(m.lines) == 0 {
		t.Fatal("setup: expected rendered lines")
	}
	first, n := &m.lines[0], len(m.lines)
	uniPress(m, 0, 0)
	for col := 1; col < 5; col++ {
		uniDrag(m, 3, col)
	}
	if !m.HasSelection() {
		t.Fatal("setup: the drag must produce a selection")
	}
	if len(m.lines) != n || &m.lines[0] != first {
		t.Error("a drag must not rebuild the cached lines")
	}
}

// TestCachedLinesCarryNoSelection: the cache is the plain render. A selected
// frame differs from it, and clearing restores it — so the highlight lives
// only in View.
func TestCachedLinesCarryNoSelection(t *testing.T) {
	m := scrollModel(t)
	fixedClock(t)
	window := func() string { return strings.Join(m.lines[m.top:m.top+m.viewHeight()], "\n") }
	plain := window()
	uniPress(m, 1, 0)
	uniDrag(m, 2, 4)
	if window() != plain {
		t.Error("the cached lines must stay selection-free while a selection lives")
	}
	if m.View() == plain {
		t.Error("View must paint the selection over the cached lines")
	}
	m.ClearSelection()
	if m.View() != plain {
		t.Error("clearing must give back the plain frame")
	}
}

// TestSelectionOverlayOnlyRestyles: the overlay changes colour, never text —
// a selected frame and a plain one strip to the same characters.
func TestSelectionOverlayOnlyRestyles(t *testing.T) {
	m := scrollModel(t)
	fixedClock(t)
	plain := ansi.Strip(m.View())
	uniPress(m, 1, 0)
	uniDrag(m, 4, 4)
	if got := ansi.Strip(m.View()); got != plain {
		t.Errorf("the overlay rewrote the text:\n%q\nwant\n%q", got, plain)
	}
}

// TestSelectionOverlayFollowsScroll: a line that scrolls into the viewport
// while the selection covers it is painted selected, even though its cached
// form was built before the selection existed. This is what a stale
// selection-baked cache would get wrong.
func TestSelectionOverlayFollowsScroll(t *testing.T) {
	m := scrollModel(t)
	fixedClock(t)
	body := m.viewHeight()
	if len(m.lines) <= 2*body {
		t.Fatalf("setup: need a document taller than two viewports, got %d lines", len(m.lines))
	}
	// Select from the top of the document well past the bottom of the window.
	uniPress(m, 0, 0)
	uniDrag(m, body+3, 4)
	m.MouseRelease()

	plainBelow := m.lines[body+1]
	m.ScrollBy(2)
	frame := m.View()
	if !strings.Contains(frame, m.selMark()) {
		t.Error("a scrolled-in covered line must render selected")
	}
	if strings.Contains(frame, plainBelow) {
		t.Error("the scrolled-in line still shows its unselected cached form")
	}
}

// selMark is the escape sequence the selection style emits, so a test can ask
// whether a frame paints one at all.
func (m *Model) selMark() string {
	return strings.TrimSuffix(m.styles().sel.Render("x"), "x"+ansi.ResetStyle)
}

// TestSideBySideOverlayPaintsOneColumn: the pinned column highlights, the
// other stays plain — #2070's side rule, now enforced in the overlay.
func TestSideBySideOverlayPaintsOneColumn(t *testing.T) {
	m := testModel(t, "one two\ngamma delta\nshared\n", "one two\ngamma DELTA\nshared\n")
	fixedClock(t)
	x, y := m.sbsCell(1, 0, true)
	m.MousePress(x, y)
	x, y = m.sbsCell(1, 11, true)
	m.MouseDrag(x, y)
	line := strings.Split(m.View(), "\n")[1]
	sel := m.selMark()
	left, right, ok := strings.Cut(line, " │ ")
	if !ok {
		t.Fatalf("no column separator in %q", line)
	}
	if strings.Contains(left, sel) {
		t.Error("the unselected column must not highlight")
	}
	if !strings.Contains(right, sel) {
		t.Error("the selected column must highlight")
	}
}

// TestSeparatorOverlaySurvivesTheCache: a fold separator highlights through
// the overlay too — its label is painted by the same per-line renderer.
func TestSeparatorOverlaySurvivesTheCache(t *testing.T) {
	m := gapModel(t)
	step := fixedClock(t)
	var sepLine int
	for line := range m.sepLines {
		sepLine = line
	}
	if strings.Contains(m.View(), m.selMark()) {
		t.Fatal("setup: nothing selected yet")
	}
	for i := 0; i < 3; i++ {
		m.MousePress(20, sepLine-m.top)
		step(50 * time.Millisecond)
	}
	if !strings.Contains(m.View(), m.selMark()) {
		t.Error("a selected separator must render highlighted")
	}
}

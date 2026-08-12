package terminal

import (
	"fmt"
	"strings"
	"testing"
)

// autoScrollModel builds a pipe terminal with a filled scrollback: 40 numbered
// lines on a 6-row pane, so most of the content sits in history and a drag
// past the pane edge has somewhere to scroll (#1821).
func autoScrollModel(t *testing.T) *Model {
	t.Helper()
	m := NewPipe("autoscroll", 21, 6, nil)
	t.Cleanup(m.Close)
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "line%02d\n", i)
	}
	m.FeedText(b.String())
	waitView(t, &m, "line40")
	waitFor(t, "scrollback filled", func() bool { return m.ScrollbackLen() >= 34 })
	return &m
}

// selSpan returns the selection's endpoints in document order.
func selSpan(m *Model) (start, end vpos) {
	start, end = m.selAnchor, m.selHead
	if end.before(start) {
		start, end = end, start
	}
	return start, end
}

// TestDragPastTopEdgeScrollsIntoScrollback (#1821): dragging above the pane
// pages into the scrollback one line per step, and the selection grows over
// the content that scrolls in — copying returns it.
func TestDragPastTopEdgeScrollsIntoScrollback(t *testing.T) {
	m := autoScrollModel(t)
	sb := m.ScrollbackLen()
	m.MousePress(0, 0)
	for i := 1; i <= 5; i++ {
		cmd := m.MouseDrag(0, -1)
		if m.scroll != i {
			t.Fatalf("after %d edge drags scroll = %d, want %d", i, m.scroll, i)
		}
		if cmd == nil {
			t.Fatalf("edge drag %d must arm the auto-scroll tick", i)
		}
	}
	start, _ := selSpan(m)
	if start.line != sb-5 {
		t.Fatalf("selection starts at virtual line %d, want %d", start.line, sb-5)
	}
	want := strings.TrimRight(m.sess.LineText(sb-5), " ")
	if got := m.SelectionText(); !strings.HasPrefix(got, want) {
		t.Fatalf("selection = %q, want it to start with the scrolled-in line %q", got, want)
	}
	// The whole dragged range copies, not just what was visible at press time.
	if lines := strings.Count(m.SelectionText(), "\n"); lines < 5 {
		t.Fatalf("selection spans %d newlines, want at least 5", lines)
	}
}

// TestDragPastBottomEdgeReturnsToLive (#1821): dragging below the pane scrolls
// back towards the live view and stops at offset 0, where the tick retires.
func TestDragPastBottomEdgeReturnsToLive(t *testing.T) {
	m := autoScrollModel(t)
	m.scroll = 3
	m.MousePress(0, 0)
	for want := 2; want >= 0; want-- {
		cmd := m.MouseDrag(0, m.h)
		if m.scroll != want {
			t.Fatalf("scroll = %d, want %d", m.scroll, want)
		}
		if want > 0 && cmd == nil {
			t.Fatal("a bottom-edge drag with history left must keep ticking")
		}
	}
	if cmd := m.MouseDrag(0, m.h); cmd != nil || m.scroll != 0 {
		t.Fatalf("at the live view the auto-scroll must stop: cmd=%v scroll=%d", cmd != nil, m.scroll)
	}
}

// TestAutoScrollTickContinuesAndEndsOnRelease (#1821): the scrolling continues
// while the pointer rests at the edge — driven by the tick, without further
// motion events — and the release retires the ticks still in flight.
func TestAutoScrollTickContinuesAndEndsOnRelease(t *testing.T) {
	m := autoScrollModel(t)
	m.MousePress(0, 0)
	if cmd := m.MouseDrag(0, -1); cmd == nil {
		t.Fatal("the edge drag must arm the auto-scroll tick")
	}
	msg := AutoScrollMsg{Key: m.SessionKey(), Gen: m.autoGen}
	for want := 2; want <= 4; want++ {
		if cmd := m.AutoScroll(msg); cmd == nil {
			t.Fatalf("tick %d must re-arm while the pointer rests at the edge", want)
		}
		if m.scroll != want {
			t.Fatalf("after tick scroll = %d, want %d", m.scroll, want)
		}
	}
	start, _ := selSpan(m)
	m.MouseRelease(0, -1)
	if cmd := m.AutoScroll(msg); cmd != nil {
		t.Fatal("a tick after the release must not re-arm")
	}
	if m.scroll != 4 {
		t.Fatalf("the retired tick must not scroll: scroll = %d, want 4", m.scroll)
	}
	if got, _ := selSpan(m); got != start {
		t.Fatalf("the selection moved after the release: %v, want %v", got, start)
	}
}

// TestAutoScrollStaleTickIgnored (#1821): a tick from another session (or from
// a drag that has been superseded) never touches the view.
func TestAutoScrollStaleTickIgnored(t *testing.T) {
	m := autoScrollModel(t)
	m.MousePress(0, 0)
	m.MouseDrag(0, -1)
	before := m.scroll
	if cmd := m.AutoScroll(AutoScrollMsg{Key: "other", Gen: m.autoGen}); cmd != nil {
		t.Fatal("a tick for another session must be dropped")
	}
	if cmd := m.AutoScroll(AutoScrollMsg{Key: m.SessionKey(), Gen: m.autoGen + 1}); cmd != nil {
		t.Fatal("a tick from a superseded drag must be dropped")
	}
	if m.scroll != before {
		t.Fatalf("a stale tick scrolled: %d, want %d", m.scroll, before)
	}
}

// TestAutoScrollKeepsLineGranularity (#951/#1821): after a triple click the
// auto-scrolling drag keeps extending whole logical lines.
func TestAutoScrollKeepsLineGranularity(t *testing.T) {
	m := autoScrollModel(t)
	sb := m.ScrollbackLen()
	for i := 0; i < 2; i++ {
		m.MousePress(0, 0)
		m.MouseRelease(0, 0)
	}
	m.MousePress(0, 0) // the third press starts the drag it extends
	if m.selMode != selLine {
		t.Fatalf("selMode = %d, want selLine after a triple click", m.selMode)
	}
	for i := 1; i <= 3; i++ {
		m.MouseDrag(0, -1)
	}
	start, _ := selSpan(m)
	if start.line != sb-3 || start.col != 0 {
		t.Fatalf("line-wise selection starts at %v, want {%d 0}", start, sb-3)
	}
	want := strings.TrimRight(m.sess.LineText(sb-3), " ")
	if got := m.SelectionText(); !strings.HasPrefix(got, want) {
		t.Fatalf("selection = %q, want the whole scrolled-in line %q first", got, want)
	}
}

// TestAutoScrollKeepsWordGranularity (#951/#1821): a double-click drag past
// the edge extends word-wise over the scrolled-in content.
func TestAutoScrollKeepsWordGranularity(t *testing.T) {
	m := autoScrollModel(t)
	sb := m.ScrollbackLen()
	m.MousePress(2, 0)
	m.MouseRelease(2, 0)
	m.MousePress(2, 0) // the second press starts the drag it extends
	if m.selMode != selWord {
		t.Fatalf("selMode = %d, want selWord after a double click", m.selMode)
	}
	for i := 1; i <= 2; i++ {
		m.MouseDrag(2, -1)
	}
	start, _ := selSpan(m)
	if start.line != sb-2 || start.col != 0 {
		t.Fatalf("word-wise selection starts at %v, want the word start {%d 0}", start, sb-2)
	}
	first := strings.TrimRight(m.sess.LineText(sb-2), " ")
	if got := m.SelectionText(); !strings.HasPrefix(got, first) {
		t.Fatalf("selection = %q, want it to start with the whole word %q", got, first)
	}
}

// TestAutoScrollLeavesMouseReportingChildAlone (#1821): the WantsMouse path is
// untouched — the drag goes to the child, the pane never scrolls.
func TestAutoScrollLeavesMouseReportingChildAlone(t *testing.T) {
	m := autoScrollModel(t)
	m.FeedText("\x1b[?1000h")
	waitFor(t, "mouse reporting enabled", func() bool { return m.sess.WantsMouse() })
	m.MousePress(0, 0)
	if m.dragging {
		t.Fatal("a mouse-reporting child must not start a selection drag")
	}
	if cmd := m.MouseDrag(0, -1); cmd != nil {
		t.Fatal("a mouse-reporting child must not arm the auto-scroll tick")
	}
	if m.scroll != 0 {
		t.Fatalf("the pane scrolled under a mouse-reporting child: %d", m.scroll)
	}
}

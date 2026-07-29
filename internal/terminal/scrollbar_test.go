package terminal

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// waitCond polls until the fed bytes have drained through the spool and cond
// holds; the pipe feed path is asynchronous.
func waitCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("fixture: %s never held", what)
}

// sbTerm builds a pipe-backed terminal whose fed lines overflow the screen by
// enough to leave n lines of scrollback.
func sbTerm(t *testing.T, n, w, h int) Model {
	t.Helper()
	m := NewPipe("sb-test", w, h, nil)
	var b strings.Builder
	for i := 0; i < n+h; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	m.FeedText(b.String())
	waitCond(t, "scrollback filled", func() bool { return m.ScrollbackLen() >= n })
	return m
}

func TestScrollbarGeometryOverScrollback(t *testing.T) {
	m := sbTerm(t, 50, 40, 10)
	sb := m.ScrollbackLen()

	// Live view: the thumb parks at the bottom of the track.
	track, total, start, length, ok := m.scrollbarGeometry()
	if !ok {
		t.Fatal("scrollback should surface a scrollbar on the live view")
	}
	if track != 10 || total != sb+10 {
		t.Errorf("geometry = track %d total %d, want 10/%d", track, total, sb+10)
	}
	if start+length != track {
		t.Errorf("live thumb = %d+%d, want flush with track end %d", start, length, track)
	}

	// Fully scrolled back: the thumb parks at the top.
	m.ScrollBy(sb)
	if _, _, start, _, _ = m.scrollbarGeometry(); start != 0 {
		t.Errorf("scrolled-back thumb start = %d, want 0", start)
	}
}

func TestScrollbarHiddenWithoutScrollback(t *testing.T) {
	m := NewPipe("sb-none", 40, 10, nil)
	m.FeedText("just one line\n")
	waitCond(t, "line rendered", func() bool { return strings.Contains(m.View(), "just one line") })
	if _, _, _, _, ok := m.scrollbarGeometry(); ok {
		t.Error("no scrollback should mean no scrollbar")
	}
	if m.ScrollbarHit(39, 5) {
		t.Error("hit without a scrollbar")
	}
}

func TestScrollbarGatedByMouseReporting(t *testing.T) {
	m := sbTerm(t, 50, 40, 10)
	// The child enables mouse reporting: the live view hides the bar so the
	// pane never steals its clicks.
	m.FeedText("\x1b[?1000h")
	waitCond(t, "mouse mode tracked", func() bool { return m.sess.WantsMouse() })
	if _, _, _, _, ok := m.scrollbarGeometry(); ok {
		t.Error("mouse-reporting child at the live view should hide the bar")
	}
	if m.ScrollbarHit(39, 5) {
		t.Error("scrollbar column must pass through to a mouse-reporting child")
	}
	// While scrolled back the pane owns the view again: bar and hits return.
	m.ScrollBy(5)
	if !m.ScrollbarHit(39, 5) {
		t.Error("scrolled back, the bar should hit again")
	}
	// The child turning mouse reporting off restores the live-view bar.
	m.ScrollBy(-5)
	m.FeedText("\x1b[?1000l")
	waitCond(t, "mouse mode released", func() bool { return !m.sess.WantsMouse() })
	if _, _, _, _, ok := m.scrollbarGeometry(); !ok {
		t.Error("plain child with scrollback should show the bar")
	}
}

func TestScrollbarHit(t *testing.T) {
	m := sbTerm(t, 50, 40, 10)
	if !m.ScrollbarHit(39, 0) || !m.ScrollbarHit(39, 9) {
		t.Error("rightmost column within the track should hit")
	}
	if m.ScrollbarHit(38, 5) {
		t.Error("non-rightmost column should not hit")
	}
	if m.ScrollbarHit(39, 10) {
		t.Error("below the track should not hit")
	}
}

func TestScrollbarPressJumpAndDrag(t *testing.T) {
	m := sbTerm(t, 50, 40, 10)
	sb := m.ScrollbackLen()

	// A press on the track's top row jumps to the oldest history.
	if m.ScrollbarPress(0) {
		t.Fatal("track press should not start a drag")
	}
	if m.Scroll() != sb {
		t.Errorf("top jump: scroll = %d, want full scrollback %d", m.Scroll(), sb)
	}

	// A press on the thumb (now parked at the top) starts a drag.
	if !m.ScrollbarPress(0) {
		t.Fatal("thumb press should start a drag")
	}

	// Dragging the thumb to the track's end returns to the live view.
	m.ScrollbarDrag(9)
	if m.Scroll() != 0 {
		t.Errorf("drag to bottom: scroll = %d, want live view 0", m.Scroll())
	}
}

func TestScrollbarRendered(t *testing.T) {
	m := sbTerm(t, 50, 40, 10)
	if !strings.Contains(m.View(), "│") {
		t.Error("scrollback should render the scrollbar track")
	}
	none := NewPipe("sb-render-none", 40, 10, nil)
	none.FeedText("short\n")
	waitCond(t, "line rendered", func() bool { return strings.Contains(none.View(), "short") })
	if strings.Contains(none.View(), "│") {
		t.Error("no scrollback should render no scrollbar")
	}
}

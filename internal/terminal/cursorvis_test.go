package terminal

import (
	"strings"
	"testing"
	"time"
)

// waitCursorHidden polls the session's DECTCEM state — the fed bytes reach the
// emulator on the session's async feed loop.
func waitCursorHidden(t *testing.T, m *Model, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.sess.CursorHidden() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("cursor hidden state never became %v", want)
}

// hasReverse reports whether the view carries a reverse-video (SGR 7) run —
// what overlayCursor paints for the pane's own cursor cell.
func hasReverse(view string) bool {
	for _, seq := range []string{"\x1b[7m", "\x1b[;7m", "\x1b[7;"} {
		if strings.Contains(view, seq) {
			return true
		}
	}
	return false
}

// TestCursorOverlayFollowsDECTCEM (#1858): a child TUI that hides the hardware
// cursor (`CSI ?25l`) paints its own, so the pane must not overlay a second
// reversed cell; `CSI ?25h` brings the overlay back.
func TestCursorOverlayFollowsDECTCEM(t *testing.T) {
	m := NewPipe("cursor-vis", 40, 6, nil)
	t.Cleanup(m.Close)
	m.SetFocused(true)

	m.FeedText("hello")
	waitView(t, &m, "hello")
	if m.sess.CursorHidden() {
		t.Fatal("a fresh session must report the cursor visible")
	}
	if !hasReverse(m.View()) {
		t.Fatalf("focused, cursor-visible pane must overlay its cursor cell:\n%q", m.View())
	}

	// Child hides the cursor: the overlay steps aside.
	m.FeedText("\x1b[?25l")
	waitCursorHidden(t, &m, true)
	if hasReverse(m.View()) {
		t.Fatalf("hidden cursor must not be overlaid:\n%q", m.View())
	}

	// …and comes back when the child shows it again.
	m.FeedText("\x1b[?25h")
	waitCursorHidden(t, &m, false)
	if !hasReverse(m.View()) {
		t.Fatalf("re-shown cursor must be overlaid again:\n%q", m.View())
	}
}

// TestCursorOverlayStillGatedOnFocus (#1858): the visibility gate is additive —
// an unfocused pane draws no cursor even while the child shows one.
func TestCursorOverlayStillGatedOnFocus(t *testing.T) {
	m := NewPipe("cursor-vis-focus", 40, 6, nil)
	t.Cleanup(m.Close)
	m.FeedText("hello")
	waitView(t, &m, "hello")
	if hasReverse(m.View()) {
		t.Fatalf("unfocused pane must not draw a cursor:\n%q", m.View())
	}
}

package terminal

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// wait_test.go holds the package's condition-based waiting (#3136): tests
// wait for the state they assert on — a grid row, a parked caller, an idle
// feed loop, an applied resize — never for a fixed stretch of wall clock.
// The one time.Sleep left is the poll interval below; every wait helper in
// the package funnels through pollUntil.

// pollInterval is how often pollUntil re-checks its condition. Short enough
// that a met condition ends the wait within a couple of milliseconds, long
// enough not to starve the goroutines the condition is waiting on.
const pollInterval = 2 * time.Millisecond

// pollUntil re-checks cond every pollInterval until it holds (true) or the
// timeout passes (false). cond is checked once more at the deadline, so a
// slow last poll never turns a met condition into a timeout.
func pollUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(pollInterval) // poll interval: cond reads plain state that has no change notification to block on
	}
}

// waitUntil fails tb unless cond holds within timeout.
func waitUntil(tb testing.TB, timeout time.Duration, what string, cond func() bool) {
	tb.Helper()
	if !pollUntil(timeout, cond) {
		tb.Fatalf("timed out after %s waiting for %s", timeout, what)
	}
}

// waitPutBlocked blocks until a producer is parked on the spool's cap — the
// state a "blocked put" test has to reach before it releases the cap.
func waitPutBlocked(t *testing.T, sp *spool) {
	t.Helper()
	waitUntil(t, 3*time.Second, "a put to block on the cap", func() bool {
		sp.mu.Lock()
		defer sp.mu.Unlock()
		return sp.putWaiters > 0
	})
}

// feedIdle reports whether everything spooled so far has gone through the
// feed loop: the spool is empty and the loop is parked in take again. The
// loop only returns to take after the emulator write and the notify of its
// last chunk, so idle means every byte is on the grid and every repaint
// decision for it has been made.
func feedIdle(s *Session) bool {
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	return len(s.out.chunks) == 0 && s.out.takeWaiters > 0
}

// waitFeedIdle blocks until the session's feed loop has consumed everything
// spooled so far (see feedIdle).
func waitFeedIdle(t *testing.T, s *Session) {
	t.Helper()
	waitUntil(t, 3*time.Second, "the feed loop to go idle", func() bool { return feedIdle(s) })
}

// assertNoRepaint fails t when the session has a repaint in flight or one
// already sent. It replaces "wait a while and see that nothing arrives":
// call it once the cause of a would-be message has run (waitFeedIdle, a
// synchronous SetParked/SetHidden) — an OutputMsg either sits in msgs
// already or is pending behind the notify timer, which notifyPending shows.
func assertNoRepaint(t *testing.T, s *Session, msgs <-chan tea.Msg, what string) {
	t.Helper()
	if s.notifyPending.Load() {
		t.Fatalf("%s: a repaint is armed", what)
	}
	select {
	case msg := <-msgs:
		t.Fatalf("%s: sent %T", what, msg)
	default:
	}
}

// setResizeQuiet shortens the session's resize debounce window to d for the
// rest of the test (#3136), so a trailing apply lands in milliseconds.
func setResizeQuiet(s *Session, d time.Duration) {
	s.mu.Lock()
	s.quietOverride = d
	s.mu.Unlock()
}

// resizeSettled reports whether the session's grid has reached w×h with no
// trailing apply still armed.
func resizeSettled(s *Session, w, h int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.resizePending && s.w == w && s.h == h
}

// waitResized blocks until the session's debounced resize to w×h applied.
func waitResized(t *testing.T, s *Session, w, h int) {
	t.Helper()
	waitUntil(t, 3*time.Second, "the resize to apply", func() bool { return resizeSettled(s, w, h) })
}

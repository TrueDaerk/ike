package app

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/terminal"
)

// collectSender returns a sender that records every re-injected message.
func collectSender() (func(tea.Msg), func() []tea.Msg) {
	var mu sync.Mutex
	var msgs []tea.Msg
	send := func(m tea.Msg) {
		mu.Lock()
		msgs = append(msgs, m)
		mu.Unlock()
	}
	snapshot := func() []tea.Msg {
		mu.Lock()
		defer mu.Unlock()
		return append([]tea.Msg(nil), msgs...)
	}
	return send, snapshot
}

func waitForFlush(t *testing.T, snapshot func() []tea.Msg) coalescedInputMsg {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range snapshot() {
			if c, ok := m.(coalescedInputMsg); ok {
				return c
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no coalescedInputMsg was flushed")
	return coalescedInputMsg{}
}

// TestNextIntervalAdaptsToRenderCost verifies the scroll flush cadence tracks the
// measured render cost (#610): cheap frames stay at the floor (~60fps), expensive
// frames back off toward the ceiling so render CPU stays bounded.
func TestNextIntervalAdaptsToRenderCost(t *testing.T) {
	defer renderNanos.Store(0)

	renderNanos.Store(int64(1 * time.Millisecond)) // cheap frame
	if d := nextInterval(); d != coalesceInterval {
		t.Fatalf("cheap frame interval = %v, want floor %v", d, coalesceInterval)
	}

	renderNanos.Store(int64(8 * time.Millisecond)) // 8ms * 3 = 24ms, between floor and ceiling
	if d := nextInterval(); d != 8*time.Millisecond*renderBudgetDivisor {
		t.Fatalf("mid frame interval = %v, want %v", d, 8*time.Millisecond*renderBudgetDivisor)
	}

	renderNanos.Store(int64(50 * time.Millisecond)) // very expensive -> clamp to ceiling
	if d := nextInterval(); d != coalesceCeiling {
		t.Fatalf("expensive frame interval = %v, want ceiling %v", d, coalesceCeiling)
	}
}

// TestFilterPassesNonMouseThrough verifies keys and other messages are never
// dropped or delayed by the coalescer.
func TestFilterPassesNonMouseThrough(t *testing.T) {
	c := NewMouseCoalescer()
	for _, msg := range []tea.Msg{
		tea.KeyPressMsg{},
		tea.MouseClickMsg{Button: tea.MouseLeft},
		tea.MouseReleaseMsg{Button: tea.MouseLeft},
		tea.WindowSizeMsg{Width: 80, Height: 24},
		"arbitrary",
	} {
		if got := c.Filter(nil, msg); got == nil {
			t.Fatalf("filter dropped a pass-through message: %T", msg)
		}
	}
}

// TestFilterAbsorbsMouseAndFlushes verifies wheel/motion events are absorbed
// (return nil, so bubbletea skips Update+render) and re-injected as one batch
// preserving every wheel notch and the latest motion.
func TestFilterAbsorbsMouseAndFlushes(t *testing.T) {
	c := NewMouseCoalescer()
	send, snapshot := collectSender()
	c.SetSender(send)

	for i := 0; i < 7; i++ {
		if got := c.Filter(nil, tea.MouseWheelMsg{Button: tea.MouseWheelDown}); got != nil {
			t.Fatalf("wheel %d not absorbed (got %T)", i, got)
		}
	}
	// Two motions: only the latest survives.
	if got := c.Filter(nil, tea.MouseMotionMsg{X: 1, Y: 1}); got != nil {
		t.Fatal("motion not absorbed")
	}
	if got := c.Filter(nil, tea.MouseMotionMsg{X: 9, Y: 5}); got != nil {
		t.Fatal("motion not absorbed")
	}

	batch := waitForFlush(t, snapshot)
	if len(batch.wheels) != 7 {
		t.Fatalf("flushed %d wheels, want 7 (no notch lost)", len(batch.wheels))
	}
	if batch.motion == nil {
		t.Fatal("flushed batch missing the motion")
	}
	if m := batch.motion.Mouse(); m.X != 9 || m.Y != 5 {
		t.Fatalf("motion = (%d,%d), want the latest (9,5)", m.X, m.Y)
	}
}

// TestFilterAbsorbsTerminalOutput guards #803: per-session OutputMsgs are
// absorbed and folded into one batch carrying the deduplicated, sorted keys,
// so N busy terminals cost one Update+render per flush instead of N per
// quiet interval.
func TestFilterAbsorbsTerminalOutput(t *testing.T) {
	c := NewMouseCoalescer()
	send, snapshot := collectSender()
	c.SetSender(send)

	for i := 0; i < 3; i++ { // repeated notifications of the same sessions
		for _, key := range []string{"terminal:2", "terminal", "terminal:3"} {
			if got := c.Filter(nil, terminal.OutputMsg{Key: key}); got != nil {
				t.Fatalf("OutputMsg %q not absorbed (got %T)", key, got)
			}
		}
	}
	batch := waitForFlush(t, snapshot)
	want := []string{"terminal", "terminal:2", "terminal:3"}
	if len(batch.termKeys) != len(want) {
		t.Fatalf("termKeys = %v, want %v", batch.termKeys, want)
	}
	for i, k := range want {
		if batch.termKeys[i] != k {
			t.Fatalf("termKeys = %v, want %v", batch.termKeys, want)
		}
	}
}

// TestFlushSingleInFlightUnderBackpressure verifies that when the event loop is
// slow to accept messages (a blocked sender, standing in for a render-bound loop
// during a sustained scroll), the coalescer keeps at most ONE flush in flight —
// it does not spawn a growing pile of blocked flush goroutines. Every wheel is
// still delivered once the consumer catches up (#602 regression guard).
func TestFlushSingleInFlightUnderBackpressure(t *testing.T) {
	c := NewMouseCoalescer()

	var mu sync.Mutex
	inFlight, maxInFlight, delivered := 0, 0, 0
	gate := make(chan struct{}) // each send blocks until handed a token
	send := func(m tea.Msg) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
		<-gate // stall like a busy loop
		mu.Lock()
		if ci, ok := m.(coalescedInputMsg); ok {
			delivered += len(ci.wheels)
		}
		inFlight--
		mu.Unlock()
	}
	c.SetSender(send)

	// A releaser drains sends slower than they are produced, so a flush is
	// usually blocked when the next wheels arrive.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case gate <- struct{}{}:
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	fired := 0
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		c.Filter(nil, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		fired++
		time.Sleep(1 * time.Millisecond)
	}

	// Let the backlog drain.
	drainBy := time.Now().Add(2 * time.Second)
	for time.Now().Before(drainBy) {
		mu.Lock()
		d := delivered
		mu.Unlock()
		if d >= fired {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(done)

	mu.Lock()
	defer mu.Unlock()
	if maxInFlight > 1 {
		t.Fatalf("max concurrent flushes = %d, want 1 (backlog pile-up)", maxInFlight)
	}
	if delivered != fired {
		t.Fatalf("delivered %d wheels, fired %d (lost or duplicated under backpressure)", delivered, fired)
	}
}

// TestFilterDoesNotReabsorbInjected verifies the re-injected batch passes through
// the filter (it must not be re-absorbed, which would loop forever).
func TestFilterDoesNotReabsorbInjected(t *testing.T) {
	c := NewMouseCoalescer()
	if got := c.Filter(nil, coalescedInputMsg{}); got == nil {
		t.Fatal("coalescedInputMsg must pass through the filter")
	}
}

// TestFlushWithoutSenderIsSafe verifies absorbing before a sender is wired does
// not panic and simply holds the events.
func TestFlushWithoutSenderIsSafe(t *testing.T) {
	c := NewMouseCoalescer()
	if got := c.Filter(nil, tea.MouseWheelMsg{Button: tea.MouseWheelUp}); got != nil {
		t.Fatal("wheel not absorbed without a sender")
	}
	c.flush() // no sender: must be a no-op, not a panic
}

// flushLatency absorbs terminal output for the given session keys and
// measures how long the coalescer takes to flush it.
func flushLatency(t *testing.T, c *MouseCoalescer, snapshot func() []tea.Msg, keys ...string) time.Duration {
	t.Helper()
	start := time.Now()
	for _, key := range keys {
		c.Filter(nil, terminal.OutputMsg{Key: key})
	}
	waitForFlush(t, snapshot)
	return time.Since(start)
}

// TestTerminalOnlyBatchBacksOffWhenUnattended verifies the unattended cadence
// (#3078): a batch holding terminal output alone, with no user input in the
// attended window, flushes no sooner than termIdleInterval (~10 fps) instead
// of the ~60 fps mouse floor.
func TestTerminalOnlyBatchBacksOffWhenUnattended(t *testing.T) {
	defer renderNanos.Store(0)
	renderNanos.Store(0) // cheap frames: the mouse floor would be 16ms
	c := NewMouseCoalescer()
	send, snapshot := collectSender()
	c.SetSender(send)
	if got := flushLatency(t, c, snapshot, "terminal", "terminal:2"); got < termIdleInterval-10*time.Millisecond {
		t.Fatalf("unattended terminal output flushed after %v, want ≥ %v", got, termIdleInterval)
	}
}

// TestRecentInputKeepsTerminalOutputAtTheNormalCadence verifies that terminal
// output following a key (its echo, typically) flushes at the mouse cadence:
// the back-off applies only once the attended window has passed (#3078).
func TestRecentInputKeepsTerminalOutputAtTheNormalCadence(t *testing.T) {
	defer renderNanos.Store(0)
	renderNanos.Store(0)
	c := NewMouseCoalescer()
	send, snapshot := collectSender()
	c.SetSender(send)
	if got := c.Filter(nil, tea.KeyPressMsg{Code: 'a'}); got == nil {
		t.Fatal("a key press must pass through")
	}
	if got := flushLatency(t, c, snapshot, "terminal"); got >= termIdleInterval-10*time.Millisecond {
		t.Fatalf("terminal output right after a key flushed after %v, want the normal cadence (< %v)", got, termIdleInterval)
	}
}

// TestUserInputPullsAnUnattendedFlushForward verifies that a key or a mouse
// event arriving while a terminal-only batch waits at the unattended cadence
// reschedules that flush to the normal cadence at once (#3078): the key is
// handled immediately by the loop, and the shell's echo does not wait out
// the back-off behind it.
func TestUserInputPullsAnUnattendedFlushForward(t *testing.T) {
	defer renderNanos.Store(0)
	renderNanos.Store(0)
	for _, tc := range []struct {
		name  string
		input tea.Msg
	}{
		{"key", tea.KeyPressMsg{Code: 'a'}},
		{"wheel", tea.MouseWheelMsg{X: 1, Y: 1, Button: tea.MouseWheelDown}},
		{"click", tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewMouseCoalescer()
			send, snapshot := collectSender()
			c.SetSender(send)
			start := time.Now()
			c.Filter(nil, terminal.OutputMsg{Key: "terminal"})
			c.mu.Lock()
			idle := c.idle
			c.mu.Unlock()
			if !idle {
				t.Fatal("test setup: the terminal-only batch arms at the unattended cadence")
			}
			c.Filter(nil, tc.input)
			batch := waitForFlush(t, snapshot)
			if got := time.Since(start); got >= termIdleInterval-10*time.Millisecond {
				t.Fatalf("flush after %s took %v, want pulled forward below %v", tc.name, got, termIdleInterval)
			}
			if len(batch.termKeys) != 1 || batch.termKeys[0] != "terminal" {
				t.Fatalf("pulled-forward batch = %+v, want the terminal key", batch)
			}
		})
	}
}

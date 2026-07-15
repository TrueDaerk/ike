package app

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

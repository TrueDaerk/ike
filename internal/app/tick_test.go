package app

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// longTick is the delay from which the test tick hook defers a timer
// instead of letting it sleep. Debounces (120–300 ms) still run for real,
// so a drained command settles the work they guard; toast expiry (4 s),
// grace periods (3 s) and idle timeouts never stall a drain (#2997).
const longTick = 500 * time.Millisecond

type deferredTick struct {
	d  time.Duration
	fn func(time.Time) tea.Msg
}

var (
	deferredMu    sync.Mutex
	deferredTicks []deferredTick
)

func init() {
	tickHook = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		if d < longTick {
			// A debounce fires at once: the drain delivers its message right
			// behind the commands already queued, which is the order the
			// program loop would see, minus the sleep.
			return func() tea.Msg { return fn(time.Now()) }
		}
		deferredMu.Lock()
		deferredTicks = append(deferredTicks, deferredTick{d: d, fn: fn})
		deferredMu.Unlock()
		// A non-nil command that delivers nothing: the handler still "armed
		// a timer" for the tests that assert it, and a drain runs it in a
		// microsecond instead of sleeping the delay.
		return func() tea.Msg { return nil }
	}
}

// realTicks restores real timers for one test: the live tests that run the
// model in a loop with a fake agent or shell on the other side need the
// polling tick to actually fire.
func realTicks(t *testing.T) {
	t.Helper()
	hook := tickHook
	tickHook = nil
	t.Cleanup(func() { tickHook = hook })
}

// takeDeferredTicks returns and clears every deferred long timer.
func takeDeferredTicks() []deferredTick {
	deferredMu.Lock()
	defer deferredMu.Unlock()
	out := deferredTicks
	deferredTicks = nil
	return out
}

// fireDeferredTicks delivers every deferred long timer that was scheduled so
// far, as if its delay had elapsed, and drains what the handlers emit. A
// test that needs a toast to expire or a grace period to lapse calls this
// instead of sleeping.
func fireDeferredTicks(t *testing.T, m Model) Model {
	t.Helper()
	now := time.Now()
	for _, tk := range takeDeferredTicks() {
		fn := tk.fn
		m = drainCmd(m, func() tea.Msg { return fn(now) })
	}
	return m
}

// TestTickHookDefersLongTimers pins the seam: a long timer never sleeps in a
// drain, a short one still fires for real.
func TestTickHookDefersLongTimers(t *testing.T) {
	takeDeferredTicks()
	start := time.Now()
	if cmd := tickCmd(4*time.Second, func(time.Time) tea.Msg { return "late" }); cmd == nil || cmd() != nil {
		t.Fatal("a long tick must be deferred: a non-nil command that delivers nothing")
	}
	if got := takeDeferredTicks(); len(got) != 1 || got[0].d != 4*time.Second {
		t.Fatalf("deferred ticks = %+v", got)
	}
	cmd := tickCmd(5*time.Millisecond, func(time.Time) tea.Msg { return "soon" })
	if cmd == nil {
		t.Fatal("a short tick must be scheduled")
	}
	if msg := cmd(); msg != "soon" {
		t.Fatalf("short tick delivered %v", msg)
	}
	if time.Since(start) > time.Second {
		t.Fatal("the hook slept through a long timer")
	}
}

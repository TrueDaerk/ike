package forge

import (
	"testing"
	"time"
)

// pollidle_test.go covers the idle backoff (#2540): a focused window nobody
// has typed into keeps polling, but ever more slowly — doubling per
// IdleBackoffAfter of silence up to MaxIdlePollInterval — and the first input
// after a long pause supersedes the stretched deadline. Fake clock, hand-fired
// deadlines, as in pollvisibility_test.go.

// idlePoller is a visible poller whose idle clock started at the fake now.
func idlePoller(t *testing.T) (*Poller, *time.Time) {
	t.Helper()
	p, now := visiblePoller(t)
	p.Input()
	return p, now
}

func TestPollerNeverToldAboutInputKeepsItsCadence(t *testing.T) {
	p, now := visiblePoller(t)
	*now = now.Add(time.Hour)
	if got := p.Delay(); got != 20*time.Second {
		t.Fatalf("a poller that never saw Input must keep the configured cadence, got %v", got)
	}
	if p.IdleStretched() {
		t.Fatal("no input report, no idle backoff")
	}
}

func TestPollerBacksOffWhileIdle(t *testing.T) {
	p, now := idlePoller(t)
	if got := p.Delay(); got != 20*time.Second {
		t.Fatalf("fresh input: cadence should be the interval, got %v", got)
	}
	base := *now
	*now = base.Add(IdleBackoffAfter - time.Second)
	if got := p.Delay(); got != 20*time.Second {
		t.Fatalf("under the idle threshold the cadence must not move, got %v", got)
	}
	want := []struct {
		idle time.Duration
		d    time.Duration
	}{
		{IdleBackoffAfter, 40 * time.Second},
		{2 * IdleBackoffAfter, 80 * time.Second},
		{3 * IdleBackoffAfter, 160 * time.Second},
		{4 * IdleBackoffAfter, 320 * time.Second},
		{5 * IdleBackoffAfter, MaxIdlePollInterval},
		{60 * IdleBackoffAfter, MaxIdlePollInterval},
	}
	for _, w := range want {
		*now = base.Add(w.idle)
		if got := p.Delay(); got != w.d {
			t.Fatalf("idle %v: want delay %v, got %v", w.idle, w.d, got)
		}
	}
	if !p.IdleStretched() {
		t.Fatal("the backoff should report itself in effect")
	}
}

func TestIdleBackoffNeverShortensALongCadence(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	p := NewPoller("/repo", time.Hour)
	p.now = func() time.Time { return now }
	p.Input()
	now = now.Add(time.Hour)
	if got := p.Delay(); got != time.Hour {
		t.Fatalf("an hour-long interval is its own ceiling, got %v", got)
	}
}

func TestIdleBackoffStacksOnTheClosedPaneCadence(t *testing.T) {
	p, now := idlePoller(t)
	p.SetPaneOpen(false)
	if got := p.Delay(); got != 100*time.Second {
		t.Fatalf("closed pane: want the slow cadence 100s, got %v", got)
	}
	base := *now
	*now = base.Add(IdleBackoffAfter)
	if got := p.Delay(); got != 200*time.Second {
		t.Fatalf("closed pane + idle: want 200s, got %v", got)
	}
	*now = base.Add(10 * IdleBackoffAfter)
	if got := p.Delay(); got != MaxIdlePollInterval {
		t.Fatalf("closed pane + long idle: want the cap, got %v", got)
	}
}

func TestInputSupersedesAnIdleStretchedDeadline(t *testing.T) {
	p, now := idlePoller(t)
	if p.Arm() == nil {
		t.Fatal("the chain should arm")
	}
	if p.Input() {
		t.Fatal("input while the deadline sits at the plain cadence owes no rearm")
	}
	// Let the armed deadline land, so the next arm happens deep into idle.
	if !fire(p) {
		t.Fatal("the plain deadline dispatches")
	}
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	*now = now.Add(3 * IdleBackoffAfter)
	if p.Arm() == nil {
		t.Fatal("Apply left the chain open; Arm schedules the stretched deadline")
	}
	stretched := p.Seq()
	if !p.IdleStretched() {
		t.Fatal("this deadline was armed under the idle backoff")
	}
	// The stretched deadline is 160s away while the listing went stale long
	// ago: the first key press must supersede it with an immediate fetch.
	if !p.Input() {
		t.Fatal("input after an idle-stretched arm must ask for a rearm")
	}
	if p.Delay() != 0 {
		t.Fatalf("the stale listing is due at once, got %v", p.Delay())
	}
	if p.Rearm() == nil {
		t.Fatal("Rearm should schedule the immediate deadline")
	}
	if p.Seq() == stretched {
		t.Fatal("the rearm must mint a new generation")
	}
	if p.Tick(PollTickMsg{Root: p.root, Seq: stretched}) {
		t.Fatal("the superseded stretched deadline must be dropped")
	}
	if !fire(p) {
		t.Fatal("the fresh deadline dispatches the catch-up fetch")
	}
	// The input reset the idle clock: after the fetch lands the cadence is
	// the plain interval again.
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	if got := p.Delay(); got != 20*time.Second {
		t.Fatalf("the idle clock restarts on input, got %v", got)
	}
	if p.Input() {
		t.Fatal("a second key press with a plain deadline pending owes nothing")
	}
}

func TestInputAfterShortIdleRearmsWithoutCatchUp(t *testing.T) {
	p, now := idlePoller(t)
	*now = now.Add(IdleBackoffAfter)
	if !fire(p) {
		t.Fatal("dispatch")
	}
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	if p.Arm() == nil {
		t.Fatal("arm the 40s deadline")
	}
	if got := p.Delay(); got != 40*time.Second {
		t.Fatalf("two idle minutes: want 40s, got %v", got)
	}
	// The listing is fresh (fetched this instant), so input supersedes the
	// stretched deadline with a plain one rather than an immediate fetch.
	if !p.Input() {
		t.Fatal("a stretched deadline owes a rearm")
	}
	if got := p.Delay(); got != 20*time.Second {
		t.Fatalf("fresh listing: plain cadence, not a catch-up, got %v", got)
	}
}

package forge

import (
	"errors"
	"testing"
	"time"
)

// pollvisibility_test.go covers the poller's visibility gates (#2488): the
// pause while the terminal is blurred, the stretched cadence while no Issues
// tool window is open, and the immediate catch-up fetch both resume with.
// Everything here runs on a fake clock and hand-fired deadlines — no test
// waits an interval out.

// visiblePoller is a poller with a controllable clock, its Issues pane open
// and one fetch already behind it: the steady state the gates change.
func visiblePoller(t *testing.T) (*Poller, *time.Time) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0)
	p := NewPoller("/repo", 20*time.Second)
	p.now = func() time.Time { return now }
	if !fire(p) {
		t.Fatal("the first deadline should dispatch a fetch")
	}
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	return p, &now
}

func TestPollerPausesWhileBlurred(t *testing.T) {
	p, now := visiblePoller(t)
	if p.Arm() == nil {
		t.Fatal("the chain should be armed while the terminal is focused")
	}

	p.Blur()
	if !p.Paused() {
		t.Fatal("a blurred terminal must pause the poll")
	}
	// The deadline armed before the blur still fires — the timer behind it
	// keeps running — but it must not dispatch a fetch.
	if fire(p) {
		t.Fatal("a deadline landing while blurred must not dispatch a fetch")
	}
	if p.Arm() != nil {
		t.Fatal("a paused poller must not arm a new deadline")
	}
	// A fetch that happened to land during the pause re-arms nothing either.
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	if p.Arm() != nil {
		t.Fatal("Apply must not reopen the chain while the pause holds")
	}

	// Back after less than one interval: nothing went stale, so the chain
	// simply picks the ordinary cadence up again.
	*now = now.Add(5 * time.Second)
	p.Focus()
	if p.Paused() {
		t.Fatal("focus must lift the pause")
	}
	if got := p.Delay(); got != 20*time.Second {
		t.Errorf("delay = %v, want the plain 20s interval — nothing was stale", got)
	}
	if p.Arm() == nil {
		t.Fatal("focus must reopen the chain")
	}
}

func TestPollerFocusFetchesAtOnceWhenStale(t *testing.T) {
	p, now := visiblePoller(t)
	p.Blur()
	fire(p) // the pending deadline is spent while blurred

	*now = now.Add(10 * time.Minute)
	p.Focus()
	if got := p.Delay(); got != 0 {
		t.Fatalf("delay after a stale pause = %v, want an immediate deadline", got)
	}
	if p.Arm() == nil {
		t.Fatal("focus must re-arm the chain")
	}
	if !fire(p) {
		t.Fatal("the immediate deadline must dispatch a fetch")
	}
	// Dispatching it clears the due flag, so the chain returns to the normal
	// cadence rather than spinning on zero-length deadlines.
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	if got := p.Delay(); got != 20*time.Second {
		t.Errorf("delay after the catch-up fetch = %v, want 20s", got)
	}
}

func TestPollerPauseCanBeSwitchedOff(t *testing.T) {
	// forge.poll_pause_on_blur = false restores the pre-#2488 behaviour: a
	// blurred terminal keeps polling exactly as before.
	p, _ := visiblePoller(t)
	p.SetPauseOnBlur(false)
	p.Blur()
	if p.Paused() {
		t.Fatal("the pause must not engage while the setting is off")
	}
	if !fire(p) {
		t.Fatal("a deadline must still dispatch a fetch with the pause off")
	}
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	if p.Arm() == nil {
		t.Fatal("the chain must keep re-arming with the pause off")
	}
}

func TestPollerNeverPausesWithoutABlurReport(t *testing.T) {
	// A terminal that does not report focus never calls Blur, so nothing
	// changes for it — the compatibility promise of #2488.
	p, _ := visiblePoller(t)
	for i := 0; i < 3; i++ {
		if p.Arm() == nil {
			t.Fatalf("round %d: the chain did not arm", i)
		}
		if !fire(p) {
			t.Fatalf("round %d: the deadline did not dispatch", i)
		}
		p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	}
}

func TestPollerSlowCadenceWhileThePaneIsClosed(t *testing.T) {
	p, _ := visiblePoller(t)
	if got := p.Cadence(); got != 20*time.Second {
		t.Fatalf("cadence with the pane open = %v, want the configured 20s", got)
	}
	p.Arm()

	if p.SetPaneOpen(false) {
		t.Fatal("closing the pane must not ask for a re-arm — the pending deadline stands")
	}
	if got := p.Cadence(); got != 100*time.Second {
		t.Errorf("cadence with the pane closed = %v, want 5×20s", got)
	}
	if got := p.Delay(); got != 100*time.Second {
		t.Errorf("delay with the pane closed = %v, want the slow cadence", got)
	}
	// The backoff still stacks on top of the slow cadence.
	p.Apply(IssuesMsg{Err: errors.New("network down")})
	if got := p.Delay(); got != 200*time.Second {
		t.Errorf("delay after one failure = %v, want 2×100s", got)
	}
}

func TestPollerSlowCadenceHasAMinimum(t *testing.T) {
	p := NewPoller("/repo", MinPollInterval) // 10s → 5× is 50s, below the floor
	p.SetPaneOpen(false)
	if got := p.Cadence(); got != MinSlowPollInterval {
		t.Errorf("cadence = %v, want the %v floor", got, MinSlowPollInterval)
	}
	// The stretch may never poll *more* often than the user asked for.
	long := NewPoller("/repo", time.Hour)
	long.SetPaneOpen(false)
	if got := long.Cadence(); got < time.Hour {
		t.Errorf("cadence = %v, want at least the configured hour", got)
	}
}

func TestPollerOpeningThePaneRefreshesAtOnce(t *testing.T) {
	p, now := visiblePoller(t)
	p.SetPaneOpen(false)
	if p.Arm() == nil {
		t.Fatal("the slow chain should be armed")
	}
	stale := PollTickMsg{Root: p.root, Seq: p.seq}

	// A whole slow cadence later the listing is stale; opening the pane must
	// fetch now and go back to the configured interval.
	*now = now.Add(100 * time.Second)
	if !p.SetPaneOpen(true) {
		t.Fatal("opening the pane must ask the caller to re-arm")
	}
	if got := p.Delay(); got != 0 {
		t.Fatalf("delay on pane open = %v, want an immediate deadline", got)
	}
	// Arm alone is idempotent and would leave the slow deadline standing;
	// Rearm is what the caller owes after SetPaneOpen said so.
	if p.Arm() != nil {
		t.Fatal("Arm must not schedule a second deadline beside the pending one")
	}
	if p.Rearm() == nil {
		t.Fatal("opening the pane must arm the immediate deadline")
	}
	// The slow deadline is superseded: its timer still fires, and dispatching
	// it would open a second chain alongside the fast one.
	if p.Tick(stale) {
		t.Fatal("the superseded slow deadline must be dropped")
	}
	if !fire(p) {
		t.Fatal("the immediate deadline must dispatch a fetch")
	}
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	if got := p.Delay(); got != 20*time.Second {
		t.Errorf("delay after the pane opened = %v, want the configured 20s", got)
	}
}

func TestPollerOpeningAFreshPaneDoesNotDoubleFetch(t *testing.T) {
	// The pane runs its own fetch on open; Refreshed records it, so the gate
	// does not ask for a second listing of the same data one moment later.
	p, now := visiblePoller(t)
	p.SetPaneOpen(false)
	p.Arm()
	*now = now.Add(100 * time.Second)
	p.Refreshed()
	if !p.SetPaneOpen(true) {
		t.Fatal("opening the pane must still supersede the slow deadline")
	}
	if got := p.Delay(); got != 20*time.Second {
		t.Errorf("delay = %v, want the ordinary interval — the pane just fetched", got)
	}
	if p.Rearm() == nil {
		t.Fatal("Rearm must replace the slow deadline with the fast one")
	}
}

func TestPollerRearmIsNilSafeAndRespectsTheGates(t *testing.T) {
	var nilp *Poller
	if nilp.Rearm() != nil {
		t.Error("a nil poller must not schedule")
	}
	p, _ := visiblePoller(t)
	p.Blur()
	if p.Rearm() != nil {
		t.Error("Rearm must obey the blur pause like Arm does")
	}
}

func TestPollerTickFromAnotherRootOrArmIsDropped(t *testing.T) {
	p, _ := visiblePoller(t)
	p.Arm()
	if p.Tick(PollTickMsg{Root: "/elsewhere", Seq: p.seq}) {
		t.Error("a tick for another project must be dropped")
	}
	if p.Tick(PollTickMsg{Root: p.root, Seq: p.seq + 1}) {
		t.Error("a tick from an arm that never happened must be dropped")
	}
	if !p.Armed() {
		t.Error("dropping a foreign tick must not clear the real pending deadline")
	}
}

func TestPollerVisibilityGatesAreNilSafe(t *testing.T) {
	var p *Poller
	p.Blur()
	p.Focus()
	p.Refreshed()
	p.SetPauseOnBlur(false)
	if p.SetPaneOpen(true) || p.Paused() || p.PaneOpen() || p.Cadence() != 0 {
		t.Error("a nil poller must report itself as doing nothing")
	}
}

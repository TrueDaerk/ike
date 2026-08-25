package forge

import (
	"errors"
	"testing"
	"time"
)

// poll_test.go covers the background polling state machine (#2085): the
// snapshot diff (seeding emits nothing, every event kind fires once), the
// skip-while-in-flight rule, the exponential backoff with its single degrade
// and recover edges, and the setup stop.

func issue(n int, title string) Issue { return Issue{Number: n, Title: title} }

func pr(n int, state string, checks CheckState) PR {
	return PR{Number: n, State: state, Checks: checks}
}

// kinds reduces an event slice to its kinds, in order.
func kinds(evs []Event) []EventKind {
	out := make([]EventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func sameKinds(got []EventKind, want ...EventKind) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestDiffIssuesOpenedAndClosed(t *testing.T) {
	prev := Snapshot{Issues: []Issue{issue(7, "seven"), issue(5, "five")}}
	next := Snapshot{Issues: []Issue{issue(9, "nine"), issue(7, "seven")}}
	evs := Diff(prev, next)
	if !sameKinds(kinds(evs), IssueOpened, IssueClosed) {
		t.Fatalf("kinds = %v, want [IssueOpened IssueClosed]", kinds(evs))
	}
	if evs[0].Number != 9 || evs[0].Title != "nine" {
		t.Errorf("opened event = %+v, want issue 9", evs[0])
	}
	if evs[1].Number != 5 {
		t.Errorf("closed event = %+v, want issue 5", evs[1])
	}
}

func TestDiffPRStateChanges(t *testing.T) {
	prev := Snapshot{PRs: []PR{pr(1, "OPEN", ChecksPassing), pr(2, "OPEN", ChecksPending), pr(3, "OPEN", ChecksPassing)}}
	next := Snapshot{PRs: []PR{
		pr(1, "MERGED", ChecksPassing),
		pr(2, "CLOSED", ChecksPending),
		pr(3, "OPEN", ChecksFailing),
		pr(4, "OPEN", ChecksNone),
	}}
	if got := kinds(Diff(prev, next)); !sameKinds(got, PRMerged, PRClosed, PRChecksFailing, PROpened) {
		t.Fatalf("kinds = %v, want [PRMerged PRClosed PRChecksFailing PROpened]", got)
	}
}

func TestDiffChecksFailingReportedOnce(t *testing.T) {
	red := Snapshot{PRs: []PR{pr(1, "OPEN", ChecksFailing)}}
	if got := kinds(Diff(red, red)); len(got) != 0 {
		t.Fatalf("a PR that stays red produced %v, want no events", got)
	}
	green := Snapshot{PRs: []PR{pr(1, "OPEN", ChecksPassing)}}
	if got := kinds(Diff(green, red)); !sameKinds(got, PRChecksFailing) {
		t.Fatalf("green→red = %v, want [PRChecksFailing]", got)
	}
	if got := kinds(Diff(red, green)); len(got) != 0 {
		t.Fatalf("red→green = %v, want no events", got)
	}
}

func TestDiffIgnoresPRsLeavingTheListing(t *testing.T) {
	// The listings are capped, so an old pull request falling off the end is
	// not a state change.
	prev := Snapshot{PRs: []PR{pr(1, "MERGED", ChecksNone), pr(2, "OPEN", ChecksNone)}}
	next := Snapshot{PRs: []PR{pr(2, "OPEN", ChecksNone)}}
	if got := kinds(Diff(prev, next)); len(got) != 0 {
		t.Fatalf("kinds = %v, want no events", got)
	}
}

func TestPollerSeedsSilently(t *testing.T) {
	p := NewPoller("/repo", 20*time.Second)
	res := p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}, PRs: []PR{pr(1, "OPEN", ChecksFailing)}})
	if !res.Seeded {
		t.Fatal("first fetch should seed")
	}
	if len(res.Events) != 0 {
		t.Fatalf("seeding emitted %v, want no events", kinds(res.Events))
	}
	if p.Snapshot() == nil {
		t.Fatal("snapshot not stored")
	}
	res = p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one"), issue(2, "two")}, PRs: []PR{pr(1, "OPEN", ChecksFailing)}})
	if res.Seeded {
		t.Fatal("second fetch should not seed")
	}
	if got := kinds(res.Events); !sameKinds(got, IssueOpened) {
		t.Fatalf("kinds = %v, want [IssueOpened]", got)
	}
}

func TestPollerKeepsPRsWhenOnlyThePRListingFailed(t *testing.T) {
	p := NewPoller("/repo", 20*time.Second)
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}, PRs: []PR{pr(1, "OPEN", ChecksNone)}})
	// A partial result must not read as "every pull request disappeared"…
	res := p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}, PRErr: errors.New("gh pr list failed")})
	if len(res.Events) != 0 {
		t.Fatalf("partial result emitted %v, want no events", kinds(res.Events))
	}
	// …nor may the next full one report the same PR as newly opened.
	res = p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}, PRs: []PR{pr(1, "OPEN", ChecksNone)}})
	if len(res.Events) != 0 {
		t.Fatalf("recovered listing emitted %v, want no events", kinds(res.Events))
	}
}

func TestPollerSeedsThePRHalfSilentlyAfterAFailedFirstPRListing(t *testing.T) {
	p := NewPoller("/repo", 20*time.Second)
	// The seeding fetch got the issues but not the pull requests, so the
	// snapshot's empty PR list is a stand-in, not an observation.
	res := p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}, PRErr: errors.New("gh pr list failed")})
	if !res.Seeded {
		t.Fatal("the first fetch must seed even when only the PR listing failed")
	}
	// The first real PR listing is that half's silent seed — reporting every
	// open pull request as newly opened here is the event storm seeding exists
	// to prevent.
	res = p.Apply(IssuesMsg{
		Issues: []Issue{issue(1, "one")},
		PRs:    []PR{pr(1, "OPEN", ChecksNone), pr(2, "MERGED", ChecksNone)},
	})
	if len(res.Events) != 0 {
		t.Fatalf("the PR half's seed emitted %v, want no events", kinds(res.Events))
	}
	// From now on the PR half is seeded and real changes do come through.
	res = p.Apply(IssuesMsg{
		Issues: []Issue{issue(1, "one")},
		PRs:    []PR{pr(1, "MERGED", ChecksNone), pr(2, "MERGED", ChecksNone)},
	})
	if got := kinds(res.Events); !sameKinds(got, PRMerged) {
		t.Fatalf("events = %v, want one PRMerged", got)
	}
}

func TestPollerSkipsTickWhileFetchInFlight(t *testing.T) {
	p := NewPoller("/repo", 20*time.Second)
	if !p.Tick() {
		t.Fatal("first tick should dispatch a fetch")
	}
	if !p.InFlight() {
		t.Fatal("poller should be in flight")
	}
	if p.Tick() {
		t.Fatal("a tick while a fetch is in flight must be dropped")
	}
	if cmd := p.Arm(); cmd != nil {
		t.Fatal("Arm must not schedule while a fetch is in flight")
	}
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	if p.InFlight() {
		t.Fatal("Apply should clear the in-flight flag")
	}
	if cmd := p.Arm(); cmd == nil {
		t.Fatal("Arm should schedule once the fetch landed")
	}
}

func TestPollerArmIsIdempotent(t *testing.T) {
	p := NewPoller("/repo", 20*time.Second)
	if p.Arm() == nil {
		t.Fatal("first Arm should schedule")
	}
	if !p.Armed() {
		t.Fatal("Armed should report the pending deadline")
	}
	if p.Arm() != nil {
		t.Fatal("a second Arm must not schedule a second deadline")
	}
	// Tick clears the pending flag, so the chain can be re-armed afterwards.
	p.Tick()
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	if p.Arm() == nil {
		t.Fatal("the chain should re-arm after a completed round")
	}
}

func TestPollerIntervalFloorAndOff(t *testing.T) {
	if got := NewPoller("/repo", 3*time.Second).Interval(); got != MinPollInterval {
		t.Errorf("interval = %v, want the %v floor", got, MinPollInterval)
	}
	off := NewPoller("/repo", 0)
	if off.Enabled() {
		t.Error("interval 0 must disable polling")
	}
	if off.Arm() != nil {
		t.Error("a disabled poller must not schedule")
	}
	if off.Tick() {
		t.Error("a disabled poller must not fetch")
	}
	off.SetInterval(30 * time.Second)
	if !off.Enabled() || off.Arm() == nil {
		t.Error("raising the interval should re-enable polling")
	}
}

func TestPollerBackoffAndRecovery(t *testing.T) {
	p := NewPoller("/repo", 20*time.Second)
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}}) // seed

	boom := errors.New("network down")
	res := p.Apply(IssuesMsg{Err: boom})
	if !res.Degraded {
		t.Fatal("the first failure should raise the degrade edge")
	}
	if got := p.Delay(); got != 40*time.Second {
		t.Errorf("delay after 1 failure = %v, want 40s", got)
	}
	for i := 0; i < 2; i++ {
		if res := p.Apply(IssuesMsg{Err: boom}); res.Degraded {
			t.Fatalf("failure %d raised a second degrade edge — that is the toast spam #2085 forbids", i+2)
		}
	}
	if got := p.Delay(); got != 160*time.Second {
		t.Errorf("delay after 3 failures = %v, want 160s", got)
	}
	// The cap holds however long the outage lasts.
	for i := 0; i < 20; i++ {
		p.Apply(IssuesMsg{Err: boom})
	}
	if got := p.Delay(); got != MaxPollBackoff {
		t.Errorf("delay = %v, want the %v cap", got, MaxPollBackoff)
	}

	res = p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one"), issue(2, "two")}})
	if !res.Recovered {
		t.Fatal("the first success should raise the recover edge")
	}
	if got := p.Delay(); got != 20*time.Second {
		t.Errorf("delay after recovery = %v, want the plain 20s interval", got)
	}
	if got := kinds(res.Events); !sameKinds(got, IssueOpened) {
		t.Fatalf("the recovering fetch diffed to %v, want [IssueOpened]", got)
	}
	if res := p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one"), issue(2, "two")}}); res.Recovered {
		t.Fatal("a second success must not raise another recover edge")
	}
}

func TestPollerBackoffNeverPollsFasterThanTheInterval(t *testing.T) {
	// An interval past the backoff cap is its own ceiling.
	p := NewPoller("/repo", time.Hour)
	p.Apply(IssuesMsg{Issues: []Issue{issue(1, "one")}})
	for i := 0; i < 5; i++ {
		p.Apply(IssuesMsg{Err: errors.New("network down")})
	}
	if got := p.Delay(); got != time.Hour {
		t.Errorf("delay = %v, want the configured 1h interval", got)
	}
}

func TestPollerStopsOnSetupProblem(t *testing.T) {
	p := NewPoller("/repo", 20*time.Second)
	res := p.Apply(IssuesMsg{Setup: "gh not found"})
	if res.Stopped != "gh not found" {
		t.Fatalf("Stopped = %q, want the setup message", res.Stopped)
	}
	if p.Enabled() {
		t.Fatal("a setup problem must switch polling off")
	}
	if p.Arm() != nil || p.Tick() {
		t.Fatal("a stopped poller must neither schedule nor fetch")
	}
	if p.Failures() != 0 {
		t.Fatal("a setup stop is not a failure run — it must not feed the backoff")
	}
	p.Resume()
	if !p.Enabled() || p.Arm() == nil {
		t.Fatal("Resume should restart polling")
	}
}

func TestPollerNilIsSafe(t *testing.T) {
	var p *Poller
	if p.Enabled() || p.Armed() || p.InFlight() || p.Tick() {
		t.Error("a nil poller must report itself as doing nothing")
	}
	if p.Arm() != nil || p.Root() != "" || p.Interval() != 0 || p.Snapshot() != nil {
		t.Error("a nil poller must answer with zero values")
	}
	p.SetInterval(time.Second)
	p.Resume()
	if len(p.Apply(IssuesMsg{}).Events) != 0 {
		t.Error("a nil poller must not produce events")
	}
}

// TestDiffCarriesTheDialogFields checks the fields the notification dialog
// (#2086) renders besides the title actually survive the diff — an event that
// lost its author or labels on the way out would show a blank dialog.
func TestDiffCarriesTheDialogFields(t *testing.T) {
	next := Snapshot{Issues: []Issue{{
		Number: 7, Title: "seven", URL: "https://e/7",
		Author: "ada", Labels: []Label{{Name: "bug"}},
	}}}
	evs := Diff(Snapshot{}, next)
	if len(evs) != 1 {
		t.Fatalf("events = %v, want one IssueOpened", kinds(evs))
	}
	e := evs[0]
	if e.Author != "ada" || len(e.Labels) != 1 || e.Labels[0].Name != "bug" {
		t.Errorf("event = %+v, want the author and labels carried through", e)
	}
	if e.URL != "https://e/7" || e.Title != "seven" {
		t.Errorf("event = %+v, want title and URL carried through", e)
	}

	prs := Snapshot{PRs: []PR{{Number: 9, Title: "nine", State: "MERGED", URL: "https://e/pr/9", Author: "bo"}}}
	evs = Diff(Snapshot{PRs: []PR{{Number: 9, Title: "nine", State: "OPEN"}}}, prs)
	if len(evs) != 1 || evs[0].Author != "bo" {
		t.Errorf("pr events = %+v, want the author carried through", evs)
	}
}

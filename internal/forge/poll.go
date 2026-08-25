package forge

// poll.go is the background polling half of the forge layer (#2085): the
// snapshot IKE keeps of a repository's issues and pull requests, the typed
// events one fresh listing produces against the previous snapshot, and the
// Poller state machine driving the app's tick chain.
//
// Package rule (unchanged): nothing here runs a subprocess from Update. The
// Poller only *decides* — should this tick dispatch a fetch, how long until
// the next one, did polling just degrade or recover — while the fetch itself
// stays the ordinary deadline-bounded PollCmd. That split is what keeps the
// Update loop off the network: a tick handler dispatches a Cmd and returns.

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Poll timing. The interval comes from forge.poll_interval_seconds; the
// floor and the ceiling are the config validator's, repeated here as
// durations so the Poller stays usable without a config.
const (
	// DefaultPollInterval is what an unset forge.poll_interval_seconds means.
	DefaultPollInterval = 20 * time.Second
	// MinPollInterval is the floor a non-zero interval is raised to.
	MinPollInterval = 10 * time.Second
	// MaxPollBackoff caps the exponential backoff after consecutive fetch
	// failures: an offline laptop retries every five minutes, not every 20
	// seconds, and recovers on the first success.
	MaxPollBackoff = 5 * time.Minute
)

// EventKind names one change a poll observed between two snapshots.
type EventKind int

const (
	// IssueOpened: an issue the previous open listing did not have.
	IssueOpened EventKind = iota
	// IssueClosed: an issue that left the open listing.
	IssueClosed
	// PROpened: a pull request that is open now and was not before.
	PROpened
	// PRMerged: a pull request that reached the merged state.
	PRMerged
	// PRClosed: a pull request closed without merging.
	PRClosed
	// PRChecksFailing: an open pull request whose CI rollup turned red.
	PRChecksFailing
)

// String is the event's stable name, used in notification text and tests.
func (k EventKind) String() string {
	switch k {
	case IssueOpened:
		return "IssueOpened"
	case IssueClosed:
		return "IssueClosed"
	case PROpened:
		return "PROpened"
	case PRMerged:
		return "PRMerged"
	case PRClosed:
		return "PRClosed"
	case PRChecksFailing:
		return "PRChecksFailing"
	}
	return "Unknown"
}

// Event is one snapshot difference, carrying enough to render a notification
// and open the thing it is about.
type Event struct {
	Kind   EventKind
	Number int
	Title  string
	URL    string
}

// EventsMsg carries one poll's diff into Update for any consumer (#2085 ships
// the events; the prominent notification surface is its own sub-issue). Root
// is the workspace root the poll ran for, so a message arriving after a
// project switch is recognisably stale.
type EventsMsg struct {
	Root   string
	Events []Event
}

// Snapshot is one observed listing state: the open issues and every pull
// request, exactly as one IssuesMsg carried them.
type Snapshot struct {
	Issues []Issue
	PRs    []PR
}

// prState normalizes a forge's state vocabulary ("open"/"OPEN"/"MERGED").
func prState(pr PR) string { return strings.ToUpper(pr.State) }

// Diff reports what changed between prev and next, in a deterministic order:
// issues first (opened before closed, in listing order), then pull requests
// in listing order. It never reports a pull request that merely *left* the
// listing — the listings are capped, so falling off the end is not an event.
func Diff(prev, next Snapshot) []Event {
	var out []Event

	before := make(map[int]bool, len(prev.Issues))
	for _, is := range prev.Issues {
		before[is.Number] = true
	}
	after := make(map[int]bool, len(next.Issues))
	for _, is := range next.Issues {
		after[is.Number] = true
	}
	for _, is := range next.Issues {
		if !before[is.Number] {
			out = append(out, Event{Kind: IssueOpened, Number: is.Number, Title: is.Title, URL: is.URL})
		}
	}
	// The listing is open issues only, so an issue that vanished from it was
	// closed — the one shape "closed" can take here.
	for _, is := range prev.Issues {
		if !after[is.Number] {
			out = append(out, Event{Kind: IssueClosed, Number: is.Number, Title: is.Title, URL: is.URL})
		}
	}

	prevPR := make(map[int]PR, len(prev.PRs))
	for _, pr := range prev.PRs {
		prevPR[pr.Number] = pr
	}
	for _, pr := range next.PRs {
		old, seen := prevPR[pr.Number]
		state := prState(pr)
		if !seen || prState(old) != state {
			// A pull request the snapshot never saw is reported in whatever
			// state it arrived in; a known one only when the state moved.
			// Reopening is an open event again, which is what a consumer
			// wants to hear.
			switch state {
			case "OPEN":
				out = append(out, Event{Kind: PROpened, Number: pr.Number, Title: pr.Title, URL: pr.URL})
			case "MERGED":
				out = append(out, Event{Kind: PRMerged, Number: pr.Number, Title: pr.Title, URL: pr.URL})
			default:
				out = append(out, Event{Kind: PRClosed, Number: pr.Number, Title: pr.Title, URL: pr.URL})
			}
		}
		// A red CI rollup is its own event, independent of the state move: it
		// is the one PR change that happens without the PR changing at all.
		// Only the transition counts, so a PR that stays red is reported once.
		if state == "OPEN" && pr.Checks == ChecksFailing && (!seen || old.Checks != ChecksFailing) {
			out = append(out, Event{Kind: PRChecksFailing, Number: pr.Number, Title: pr.Title, URL: pr.URL})
		}
	}
	return out
}

// PollTickMsg is one poll deadline for the repository at Root. The tick
// handler only dispatches PollCmd — it never waits on it.
type PollTickMsg struct {
	Root string
}

// PollResult is what one finished background fetch means for the app: the
// events to publish, and the two one-shot notification edges (#2085 asks for
// at most one toast when polling degrades and one when it recovers).
type PollResult struct {
	// Events are the snapshot differences; empty while seeding.
	Events []Event
	// Seeded reports the fetch that established the first snapshot. It emits
	// no events on purpose: a startup or project switch must not replay the
	// whole backlog as "new".
	Seeded bool
	// Degraded is the falling edge: this failure was the first of a run.
	Degraded bool
	// Recovered is the rising edge: this success ended a run of failures.
	Recovered bool
	// Stopped carries the setup message that switched polling off — the forge
	// is unavailable in a way the user fixes outside IKE, so retrying on a
	// timer is pointless noise.
	Stopped string
}

// Poller is the background polling state machine for one workspace root. It
// is created per root (a project switch builds a new one, which re-seeds
// silently) and shared by pointer across the value model's copies.
//
// The whole point is that no method blocks: Tick reports whether to dispatch
// a fetch, Apply folds the finished one back in, and Arm hands back the
// tea.Tick for the next deadline.
type Poller struct {
	root     string
	interval time.Duration // 0 = polling off
	armed    bool          // a PollTickMsg is pending
	inFlight bool          // a fetch is running
	stopped  string        // setup message; polling off until Resume

	snap      *Snapshot // nil until the first successful fetch seeds it
	prsSeeded bool      // the snapshot's PRs came from a real listing, not a PRErr
	failures  int       // consecutive fetch failures, driving the backoff
	degraded  bool      // a degrade notification is already out
}

// NewPoller returns the poller for root, polling every interval. An interval
// of 0 means polling off; anything between 0 and MinPollInterval is raised to
// the floor, mirroring the config validator.
func NewPoller(root string, interval time.Duration) *Poller {
	p := &Poller{root: root}
	p.SetInterval(interval)
	return p
}

// Root is the workspace root this poller polls (message routing, tests).
func (p *Poller) Root() string {
	if p == nil {
		return ""
	}
	return p.root
}

// SetInterval applies a (possibly reloaded) interval, clamping it as
// NewPoller does. Turning polling back on does not clear a setup stop — an
// unavailable forge stays unavailable until Resume says otherwise.
func (p *Poller) SetInterval(d time.Duration) {
	if p == nil {
		return
	}
	switch {
	case d <= 0:
		p.interval = 0
	case d < MinPollInterval:
		p.interval = MinPollInterval
	default:
		p.interval = d
	}
}

// Interval is the effective poll interval, 0 when polling is off (tests).
func (p *Poller) Interval() time.Duration {
	if p == nil {
		return 0
	}
	return p.interval
}

// Enabled reports whether polling should be running: configured on and not
// stopped by a setup problem.
func (p *Poller) Enabled() bool {
	return p != nil && p.interval > 0 && p.stopped == ""
}

// Stopped is the setup message that switched polling off, "" while it runs.
func (p *Poller) Stopped() string {
	if p == nil {
		return ""
	}
	return p.stopped
}

// Resume clears a setup stop so polling starts over — the recovery path for
// "install gh, press r, it works now". Callers hand it a successful
// foreground refresh; it is a no-op when polling was never stopped.
func (p *Poller) Resume() {
	if p == nil {
		return
	}
	p.stopped = ""
}

// Failures is the current consecutive-failure count (tests).
func (p *Poller) Failures() int {
	if p == nil {
		return 0
	}
	return p.failures
}

// Snapshot returns the last seeded snapshot, nil before the first success
// (tests).
func (p *Poller) Snapshot() *Snapshot {
	if p == nil {
		return nil
	}
	return p.snap
}

// Delay is the wait before the next fetch: the configured interval, doubled
// per consecutive failure and capped at MaxPollBackoff. A success resets the
// count, so recovery is immediate rather than gradual. An interval already
// longer than the cap is its own ceiling — backing off must never poll more
// often than the user asked for.
func (p *Poller) Delay() time.Duration {
	if p == nil || p.interval <= 0 {
		return 0
	}
	limit := MaxPollBackoff
	if p.interval > limit {
		limit = p.interval
	}
	d := p.interval
	for i := 0; i < p.failures; i++ {
		if d >= limit/2 {
			return limit
		}
		d *= 2
	}
	return d
}

// Arm schedules the next deadline, or returns nil when there is nothing to
// schedule: polling off, a tick already pending, or a fetch still in flight
// (Apply re-arms once it lands). Callers may call it after every message —
// it is idempotent by design, which is what makes "start polling again after
// a settings change" a one-liner.
func (p *Poller) Arm() tea.Cmd {
	if !p.Enabled() || p.armed || p.inFlight {
		return nil
	}
	p.armed = true
	root, delay := p.root, p.Delay()
	return tea.Tick(delay, func(time.Time) tea.Msg { return PollTickMsg{Root: root} })
}

// Armed reports whether a deadline is pending (the performance HUD's armed
// ticker count, tests).
func (p *Poller) Armed() bool { return p != nil && p.armed }

// InFlight reports whether a fetch is running (tests).
func (p *Poller) InFlight() bool { return p != nil && p.inFlight }

// Tick handles one deadline and reports whether the caller should dispatch
// PollCmd now. A tick that arrives while the previous fetch is still running
// is dropped rather than queued — a forge slower than the interval must not
// build a backlog of subprocesses.
func (p *Poller) Tick() bool {
	if p == nil {
		return false
	}
	p.armed = false
	if !p.Enabled() || p.inFlight {
		return false
	}
	p.inFlight = true
	return true
}

// Apply folds one finished background fetch in and reports what it means.
// It never notifies or renders itself — the app maps the result onto toasts
// and messages — so the whole state machine stays testable without a program.
func (p *Poller) Apply(msg IssuesMsg) PollResult {
	if p == nil {
		return PollResult{}
	}
	p.inFlight = false
	switch {
	case msg.Setup != "":
		// Not transient: the forge is unavailable in a way the user fixes
		// outside the pane. Stop polling instead of retrying on a timer.
		p.stopped = msg.Setup
		p.degraded = false
		p.failures = 0
		return PollResult{Stopped: msg.Setup}
	case msg.Err != nil:
		p.failures++
		res := PollResult{}
		if !p.degraded {
			p.degraded = true
			res.Degraded = true
		}
		return res
	}

	res := PollResult{}
	if p.degraded {
		p.degraded = false
		res.Recovered = true
	}
	p.failures = 0

	next := Snapshot{Issues: msg.Issues, PRs: msg.PRs}
	// The PR half seeds separately from the issue half, because it can fail on
	// its own: prsSeeded says whether the snapshot's PRs were ever a real
	// listing rather than the empty stand-in a first-fetch PRErr left behind.
	prsSeeded := p.prsSeeded
	if msg.PRErr != nil {
		// The issue listing succeeded and only the PR listing failed
		// (fetchListing degrades that way). Carrying the previous PRs forward
		// keeps the next successful poll from reporting every pull request as
		// newly opened.
		if p.snap != nil {
			next.PRs = p.snap.PRs
		}
	} else {
		p.prsSeeded = true
	}
	if p.snap == nil {
		p.snap = &next
		res.Seeded = true
		return res
	}
	prev := *p.snap
	if !prsSeeded {
		// The first fetch's PR listing failed, so the snapshot never held a
		// real one and this is the PR half's silent seed — not a burst of pull
		// requests that all opened at once.
		prev.PRs = next.PRs
	}
	res.Events = Diff(prev, next)
	p.snap = &next
	return res
}

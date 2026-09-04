package forge

// poll.go is the background polling half of the forge layer (#2085): the
// snapshot IKE keeps of a repository's issues and pull requests, the diff that
// turns one fresh listing into the typed events of events.go, and the Poller
// state machine driving the app's tick chain.
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
	// SlowPollFactor stretches the interval while no Issues tool window is
	// open (#2488). Nobody is reading the listing then — only the status-line
	// unread badge depends on it — so the poll keeps running, just far less
	// often.
	SlowPollFactor = 5
	// MinSlowPollInterval is the floor of that stretched cadence: even the
	// smallest configured interval drops to at most one poll a minute while
	// the pane is closed.
	MinSlowPollInterval = 60 * time.Second
)

// Snapshot is one observed listing state: the open issues and every pull
// request, exactly as one IssuesMsg carried them.
type Snapshot struct {
	Issues []Issue
	PRs    []PR
}

// prState normalizes a forge's state vocabulary ("open"/"OPEN"/"MERGED").
func prState(pr PR) string { return strings.ToUpper(pr.State) }

// issueEvent builds one issue event, carrying the author and labels the
// notification dialog (#2086) shows besides the title. A backend that does
// not report them leaves them zero and the dialog hides those rows.
func issueEvent(k EventKind, is Issue) Event {
	return Event{Kind: k, Number: is.Number, Title: is.Title, Author: is.Author, Labels: is.Labels, URL: is.URL}
}

// prEvent is issueEvent for a pull request; PR listings carry no labels.
func prEvent(k EventKind, pr PR) Event {
	return Event{Kind: k, Number: pr.Number, Title: pr.Title, Author: pr.Author, URL: pr.URL}
}

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
			out = append(out, issueEvent(IssueOpened, is))
		}
	}
	// The listing is open issues only, so an issue that vanished from it was
	// closed — the one shape "closed" can take here.
	for _, is := range prev.Issues {
		if !after[is.Number] {
			out = append(out, issueEvent(IssueClosed, is))
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
				out = append(out, prEvent(PROpened, pr))
			case "MERGED":
				out = append(out, prEvent(PRMerged, pr))
			default:
				out = append(out, prEvent(PRClosed, pr))
			}
		}
		// A red CI rollup is its own event, independent of the state move: it
		// is the one PR change that happens without the PR changing at all.
		// Only the transition counts, so a PR that stays red is reported once.
		if state == "OPEN" && pr.Checks == ChecksFailing && (!seen || old.Checks != ChecksFailing) {
			out = append(out, prEvent(PRChecksFailing, pr))
		}
	}
	return out
}

// PollTickMsg is one poll deadline for the repository at Root. The tick
// handler only dispatches PollCmd — it never waits on it.
type PollTickMsg struct {
	Root string
	// Seq identifies the Arm that scheduled this deadline. A deadline can be
	// superseded — opening the Issues pane replaces a slow-cadence one with a
	// normal-cadence one (#2488) — and the timer behind the old one keeps
	// running either way, so the stale message has to be recognizable. Tick
	// drops one whose Seq is no longer the current arm; without that the
	// superseded tick would open a second, parallel chain.
	Seq int
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
	seq      int           // arm generation, carried by PollTickMsg
	inFlight bool          // a fetch is running
	stopped  string        // setup message; polling off until Resume

	// Visibility gates (#2488). Neither is a *reason* to poll — they only say
	// how much of the configured cadence is worth spending on data nobody is
	// looking at. Both default to "visible", so a caller that never reports
	// either (a terminal without focus events, a bare test poller) polls
	// exactly as it did before.
	pauseOnBlur bool      // forge.poll_pause_on_blur
	blurred     bool      // the terminal reported blur and no focus since
	paneOpen    bool      // an Issues tool window is open in this project
	due         bool      // the next deadline is immediate (focus / pane open)
	lastFetch   time.Time // when the last fetch was dispatched
	now         func() time.Time

	snap      *Snapshot // nil until the first successful fetch seeds it
	prsSeeded bool      // the snapshot's PRs came from a real listing, not a PRErr
	failures  int       // consecutive fetch failures, driving the backoff
	degraded  bool      // a degrade notification is already out
}

// NewPoller returns the poller for root, polling every interval. An interval
// of 0 means polling off; anything between 0 and MinPollInterval is raised to
// the floor, mirroring the config validator.
func NewPoller(root string, interval time.Duration) *Poller {
	p := &Poller{root: root, pauseOnBlur: true, paneOpen: true}
	p.SetInterval(interval)
	return p
}

// clock is the poller's time source, overridable in tests so the staleness
// check can be driven without sleeping.
func (p *Poller) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
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

// SetPauseOnBlur applies forge.poll_pause_on_blur (#2488). Switching it off
// restores the pre-#2488 behaviour immediately, blur report or not.
func (p *Poller) SetPauseOnBlur(on bool) {
	if p == nil {
		return
	}
	p.pauseOnBlur = on
}

// paused reports whether the terminal is blurred and the pause is armed. It
// gates Arm and Tick rather than Enabled: polling is still *configured* on,
// it just has nobody to render for.
func (p *Poller) paused() bool { return p != nil && p.pauseOnBlur && p.blurred }

// Paused reports the blur pause (perf HUD, tests).
func (p *Poller) Paused() bool { return p.paused() }

// Blur records that the terminal lost focus (tea.BlurMsg): no further
// deadline is armed, and the one still pending is dropped when its timer
// fires. A terminal that never reports focus never blurs, so it polls exactly
// as it did before.
func (p *Poller) Blur() {
	if p == nil {
		return
	}
	p.blurred = true
}

// Focus records that the terminal regained focus (tea.FocusMsg) and lifts the
// pause. A poll whose deadline passed unnoticed is due at once — the point of
// coming back to the window is seeing current data — so the deadline the
// caller arms next fires immediately rather than a whole interval later.
func (p *Poller) Focus() {
	if p == nil {
		return
	}
	p.blurred = false
	if p.stale() {
		p.due = true
		// A deadline armed before the pause may sit at the slow cadence;
		// dropping the flag lets the caller's Arm supersede it, and Seq
		// invalidates the old message when its timer fires.
		p.armed = false
	}
}

// SetPaneOpen reports whether an Issues tool window is open in the current
// project (#2488). While none is, the cadence stretches to SlowPollFactor ×
// the interval so the unread badge still moves without re-reading the forge
// three times a minute. It returns whether the caller must Rearm: the pending
// deadline sits at the slow cadence and has to be superseded, and the listing
// is refetched at once when that cadence let it go stale.
func (p *Poller) SetPaneOpen(open bool) bool {
	if p == nil || p.paneOpen == open {
		return false
	}
	p.paneOpen = open
	if !open {
		// Closing the pane only stretches the *next* deadline; the pending one
		// is already no later than the slow cadence, so it stands.
		return false
	}
	if p.stale() {
		p.due = true
	}
	return true
}

// PaneOpen reports the pane gate's state (tests).
func (p *Poller) PaneOpen() bool { return p != nil && p.paneOpen }

// Refreshed records a listing fetch dispatched outside the poll chain — the
// pane's own 'r' or its on-open load. It counts for staleness, so returning
// to a window whose pane just refreshed does not fetch the same listing twice.
func (p *Poller) Refreshed() {
	if p == nil {
		return
	}
	p.lastFetch = p.clock()
	p.due = false
}

// stale reports whether the last fetch is older than the configured interval
// — the "is it worth fetching right now" question the focus and pane-open
// edges ask. It deliberately measures against the interval and not against
// the stretched cadence: the user just said they are looking.
func (p *Poller) stale() bool {
	if !p.Enabled() {
		return false
	}
	if p.lastFetch.IsZero() {
		return true
	}
	return p.clock().Sub(p.lastFetch) >= p.interval
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

// Delay is the wait before the next fetch: the current cadence (the
// configured interval, or the stretched one while the Issues pane is closed),
// doubled per consecutive failure and capped at MaxPollBackoff. It is 0 when
// a fetch is due at once — focus or pane-open found the listing stale. A
// success resets the failure count, so recovery is immediate rather than
// gradual. A cadence already longer than the cap is its own ceiling —
// backing off must never poll more often than the user asked for.
func (p *Poller) Delay() time.Duration {
	if p == nil || p.interval <= 0 {
		return 0
	}
	if p.due {
		// Focus or a freshly opened pane: fetch now, not one cadence from now.
		return 0
	}
	base := p.cadence()
	limit := MaxPollBackoff
	if base > limit {
		limit = base
	}
	d := base
	for i := 0; i < p.failures; i++ {
		if d >= limit/2 {
			return limit
		}
		d *= 2
	}
	return d
}

// cadence is the un-backed-off wait: the configured interval while an Issues
// pane is open, the stretched one while none is (#2488). The stretch can only
// ever slow polling down — it multiplies the interval and the floor it is
// raised to is a minute, which the smallest configured interval (10 s) is well
// below — so a closed pane never costs the forge more requests.
func (p *Poller) cadence() time.Duration {
	if p.paneOpen {
		return p.interval
	}
	d := SlowPollFactor * p.interval
	if d < MinSlowPollInterval {
		d = MinSlowPollInterval
	}
	return d
}

// Cadence is the current un-backed-off wait between polls (tests, HUD).
func (p *Poller) Cadence() time.Duration {
	if p == nil || p.interval <= 0 {
		return 0
	}
	return p.cadence()
}

// Arm schedules the next deadline, or returns nil when there is nothing to
// schedule: polling off, a tick already pending, or a fetch still in flight
// (Apply re-arms once it lands). Callers may call it after every message —
// it is idempotent by design, which is what makes "start polling again after
// a settings change" a one-liner.
func (p *Poller) Arm() tea.Cmd {
	if !p.Enabled() || p.paused() || p.armed || p.inFlight {
		return nil
	}
	p.armed = true
	p.seq++
	root, seq, delay := p.root, p.seq, p.Delay()
	return tea.Tick(delay, func(time.Time) tea.Msg { return PollTickMsg{Root: root, Seq: seq} })
}

// Rearm replaces the pending deadline with one at the current cadence: it
// forgets the pending arm and schedules afresh, which bumps Seq and so makes
// Tick drop the superseded message when its timer finally fires. Arm alone
// cannot do this — it is idempotent on purpose.
func (p *Poller) Rearm() tea.Cmd {
	if p == nil {
		return nil
	}
	p.armed = false
	return p.Arm()
}

// Armed reports whether a deadline is pending (the performance HUD's armed
// ticker count, tests).
func (p *Poller) Armed() bool { return p != nil && p.armed }

// Seq is the current arm generation — the value the pending PollTickMsg
// carries, so a test can deliver the deadline the poller actually armed.
func (p *Poller) Seq() int {
	if p == nil {
		return 0
	}
	return p.seq
}

// InFlight reports whether a fetch is running (tests).
func (p *Poller) InFlight() bool { return p != nil && p.inFlight }

// Tick handles one deadline and reports whether the caller should dispatch
// PollCmd now. A tick that arrives while the previous fetch is still running
// is dropped rather than queued — a forge slower than the interval must not
// build a backlog of subprocesses. A tick for another root, for a superseded
// arm, or one arriving while the terminal is blurred (#2488) is dropped too.
func (p *Poller) Tick(msg PollTickMsg) bool {
	if p == nil || msg.Root != p.root || msg.Seq != p.seq {
		// Another project's leftover, or a deadline superseded by an
		// immediate one — either way it must not open a second chain.
		return false
	}
	p.armed = false
	if !p.Enabled() || p.paused() || p.inFlight {
		// While blurred the deadline is simply dropped; Focus re-arms the
		// chain, which is why nothing here re-schedules.
		return false
	}
	p.due = false
	p.inFlight = true
	p.lastFetch = p.clock()
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

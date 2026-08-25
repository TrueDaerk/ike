package app

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/host"
)

// forgepoll.go is the app half of background forge polling (#2085): one
// forge.Poller per workspace root, driven by a tick chain whose handler only
// *dispatches* the fetch command. The Update loop therefore never waits on
// the forge — a poll against a stalled network costs one armed timer, not a
// frozen UI — and a tick arriving while the previous fetch is still running
// is dropped instead of queued.
//
// What the poll produces: the fresh listing goes into the Issues tool window
// (so its content stays current without pressing 'r'), and the difference
// against the previous snapshot goes out as a forge.EventsMsg for any
// consumer. The prominent notification surface for those events is its own
// sub-issue; this file only publishes them.
//
// See wiki/architecture/forge.md for the lifecycle.

// forgePollState holds the poll service across the value model's copies (the
// vcsState pattern). The poller itself is per workspace root: the model is
// rebuilt on a project switch, so the new project starts from a silent seed
// rather than reporting its whole backlog as new.
type forgePollState struct {
	poller *forge.Poller
	// rearm asks the settled pass of Update to pick the chain back up. Only a
	// config reload that re-enabled polling sets it: the chain is otherwise
	// self-sustaining (Init starts it, each finished fetch continues it), and
	// arming from every settled pass would mean every Update pass returns a
	// pending tick — an app that never quiesces.
	rearm bool
}

// forgePollInterval reads forge.poll_interval_seconds from the live config,
// so a settings edit applies on the next reload without a restart. 0 means
// polling off; an unset (or, impossibly after validation, unparsable) value
// falls back to the default. The floor is the Poller's.
func forgePollInterval(cfg host.Config) time.Duration {
	if cfg == nil {
		return forge.DefaultPollInterval
	}
	v, ok := cfg.Get("forge.poll_interval_seconds")
	if !ok {
		return forge.DefaultPollInterval
	}
	secs, err := strconv.Atoi(v)
	if err != nil {
		return forge.DefaultPollInterval
	}
	if secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// forgePoller resolves the active poller, nil in models built without one
// (bare test literals).
func (m Model) forgePoller() *forge.Poller {
	if m.forgePoll == nil {
		return nil
	}
	return m.forgePoll.poller
}

// forgeRoot is the directory the poll fetches for: the root the poller was
// built with, which is the project directory the process chdir'd into.
func (m Model) forgeRoot() string { return m.forgePoller().Root() }

// StartForgePoll opens the tick chain for the current root. It rides the
// StartWatcher lifecycle — main.go once at startup, switch.go again per
// project switch — and deliberately *not* Init, for the same reason the
// watcher does not: it keeps the background service out of tests.
//
// That is not merely tidiness here. Init's commands are drained synchronously
// by the app test helpers (`sizedWith` calls each `cmd()` in-line), and a
// poll deadline is a `tea.Tick` — draining one blocks the caller for a whole
// interval. Arming from Init therefore cost every helper-built model 20
// seconds of real time and timed the package out.
//
// The first deadline is waited out on its own goroutine and delivered through
// the host's Send, mirroring StartWatcher's `go m.host.Send(...)`. From then
// on the chain sustains itself inside Update: each finished fetch arms the
// next deadline as an ordinary returned command.
func (m Model) StartForgePoll() {
	cmd := m.forgePoller().Arm()
	if cmd == nil {
		return
	}
	go func() {
		if msg := cmd(); msg != nil {
			m.host.Send(msg)
		}
	}()
}

// armForgePoll is the settled-pass hook, and it only fires on the one edge the
// self-sustaining chain cannot cover itself: a config reload that turned
// polling back on. Everything else re-arms where it belongs — Init at the
// start, applyForgeListing after each finished fetch — so an ordinary Update
// pass adds no pending command.
func (m *Model) armForgePoll() tea.Cmd {
	if m.forgePoll == nil || !m.forgePoll.rearm {
		return nil
	}
	m.forgePoll.rearm = false
	return m.forgePoller().Arm()
}

// forgePollTick handles one deadline: dispatch the fetch (never wait on it)
// and, when the tick was dropped because a fetch is still in flight, let the
// settled pass re-arm once that one lands. A tick for another root is a
// leftover from the project switched away from — drop it.
func (m *Model) forgePollTick(msg forge.PollTickMsg) tea.Cmd {
	p := m.forgePoller()
	if p == nil || p.Root() != msg.Root {
		return nil
	}
	if !p.Tick() {
		return nil
	}
	return forge.PollCmd(msg.Root)
}

// applyForgeListing routes one finished listing — background poll or the
// pane's own 'r' — into the Issues window, and folds a poll result into the
// poll service: events out, backoff and the degrade/recover notifications in.
func (m *Model) applyForgeListing(msg forge.IssuesMsg) tea.Cmd {
	m.fillIssuesPanel(msg)
	p := m.forgePoller()
	if p == nil {
		return nil
	}
	if !msg.Poll {
		// A foreground refresh that found the forge is the recovery path out
		// of a setup stop: install gh, press 'r', polling resumes — and the
		// chain, stopped since the setup problem, has to be reopened here.
		if msg.Setup == "" && msg.Err == nil {
			p.Resume()
			return p.Arm()
		}
		return nil
	}
	res := p.Apply(msg)
	var cmds []tea.Cmd
	switch {
	case res.Degraded:
		// Exactly one toast per failure run, never one per failed poll: the
		// backoff keeps retrying quietly and the recovery below says when it
		// worked again.
		m.host.Notify(host.Warn, "forge polling degraded: "+msg.Err.Error())
	case res.Recovered:
		m.host.Notify(host.Info, "forge polling recovered")
	}
	if len(res.Events) > 0 {
		out := forge.EventsMsg{Root: p.Root(), Events: res.Events}
		cmds = append(cmds, func() tea.Msg { return out })
	}
	if arm := p.Arm(); arm != nil {
		cmds = append(cmds, arm)
	}
	return tea.Batch(cmds...)
}

// reconfigureForgePoll applies a [forge] change on a live config reload. A new
// interval takes effect on the next deadline and switching polling off lets
// the pending tick expire unused — neither needs a command. Switching it back
// on does: the chain has no deadline left to continue from, so the settled
// pass is asked to reopen it.
func (m *Model) reconfigureForgePoll(cfg host.Config) {
	p := m.forgePoller()
	if p == nil {
		return
	}
	was := p.Enabled()
	p.SetInterval(forgePollInterval(cfg))
	if !was && p.Enabled() {
		m.forgePoll.rearm = true
	}
}

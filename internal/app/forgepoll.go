package app

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/host"
	"ike/internal/pane"
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

// forgeCacheEnabled reads forge.cache (#2108) from the live config: the
// persistent listing cache is on unless it is explicitly switched off.
func forgeCacheEnabled(cfg host.Config) bool {
	if cfg == nil {
		return true
	}
	v, ok := cfg.Get("forge.cache")
	return !ok || v != "false"
}

// forgePausePollOnBlur reads forge.poll_pause_on_blur (#2488): the poll
// pauses while the terminal has no focus unless it is explicitly switched
// off, which restores the always-polling behaviour of #2085.
func forgePausePollOnBlur(cfg host.Config) bool {
	if cfg == nil {
		return true
	}
	v, ok := cfg.Get("forge.poll_pause_on_blur")
	return !ok || v != "false"
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

// armForgePoll is the settled-pass hook, and it only fires on edges the
// self-sustaining chain cannot cover itself: a config reload that turned
// polling back on, and the Issues tool window opening, which supersedes the
// slow-cadence deadline it was running on (#2488). Everything else re-arms
// where it belongs — Init at the start, applyForgeListing after each finished
// fetch — so an ordinary Update pass adds no pending command. The pane edge
// adds none either: its deadline goes out through sendForgeRearm.
func (m *Model) armForgePoll() tea.Cmd {
	if m.forgePoll == nil {
		return nil
	}
	// The pane gate (#2488) is read here rather than pushed from every place
	// a tool window can appear or vanish (toggle, layout restore, project
	// switch, pane close): Has() is a map lookup, and SetPaneOpen only reports
	// work on the edge, so an ordinary pass costs nothing.
	if m.forgePoller().SetPaneOpen(m.activeWS().Panes.Has(pane.IssuesKey)) {
		m.sendForgeRearm()
	}
	if !m.forgePoll.rearm {
		return nil
	}
	m.forgePoll.rearm = false
	return m.forgePoller().Arm()
}

// sendForgeRearm supersedes the pending deadline with one at the cadence the
// pane just restored, and — the load-bearing part — delivers it through the
// host's Send on its own goroutine instead of returning it as a command.
//
// A settled pass may not hand a poll deadline back to Update: the app test
// helpers drain Update's commands *synchronously*, and the poll chain is
// self-sustaining, so a drainer that enters it never comes back out (it waits
// out a deadline, dispatches the fetch, waits out the next one, forever).
// StartForgePoll's first deadline rides the same goroutine for the same
// reason.
func (m *Model) sendForgeRearm() {
	cmd := m.forgePoller().Rearm()
	if cmd == nil {
		return
	}
	h := m.host
	go func() {
		if msg := cmd(); msg != nil {
			h.Send(msg)
		}
	}()
}

// forgePollTick handles one deadline: dispatch the fetch (never wait on it)
// and, when the tick was dropped because a fetch is still in flight, let the
// settled pass re-arm once that one lands. The Poller does the dropping — a
// tick for another root is a leftover from the project switched away from, a
// tick for a superseded arm or one arriving while the terminal is blurred
// (#2488) is spent — so all this has to decide is whether to fetch.
func (m *Model) forgePollTick(msg forge.PollTickMsg) tea.Cmd {
	p := m.forgePoller()
	if !p.Tick(msg) {
		return nil
	}
	return forge.PollCmd(msg.Root)
}

// forgeFocus and forgeBlur ride the terminal's focus reports (#2488). The
// blurred half of that pair only sets a flag — the pending deadline is
// dropped when it fires, and no new one is armed — while the focused half
// re-opens the chain, immediately when the pause outlasted one interval.
// A terminal that never reports focus never calls either, so it polls exactly
// as it did before.
func (m *Model) forgeFocus() tea.Cmd {
	p := m.forgePoller()
	if p == nil {
		return nil
	}
	p.Focus()
	return p.Arm()
}

func (m *Model) forgeBlur() {
	m.forgePoller().Blur()
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
			// A fresh listing is a fresh listing whoever asked for it: it
			// counts for the staleness check (#2488), so returning to the
			// window right after pressing 'r' does not re-fetch it.
			p.Refreshed()
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
	// The cache toggle (#2108) rides the same reload: the forge package reads
	// it at fetch time, so the next listing honors the new value immediately.
	forge.SetCacheEnabled(forgeCacheEnabled(cfg))
	p := m.forgePoller()
	if p == nil {
		return
	}
	was := p.Enabled()
	p.SetInterval(forgePollInterval(cfg))
	p.SetPauseOnBlur(forgePausePollOnBlur(cfg))
	if !was && p.Enabled() {
		m.forgePoll.rearm = true
	}
}

package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agenttrace"
	"ike/internal/changefeed"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/tracepanel"
)

// agenttrace_panel.go wires the Agent Trace tool window (#2840, epic 0540):
// a singleton pane showing the coding-agent session of a tool pane as a
// turn → assistant decision → tool call → file tree (internal/tracepanel on
// hiertree). It follows the focused — else the most recently focused — agent
// tool pane: the session comes from the hook binding of that terminal
// (agenthooks.go) or, without one, from discovery on its working directory;
// with no terminal at all the project root is scanned.
//
// Reading happens off the Update loop: one agenttrace.Reader tails the
// transcript, a one-second ticker drives incremental reads while the pane is
// open, and every read hands the panel a freshly grouped tree whose node
// keys are stable, so the pane keeps expansion and selection across updates.
// File rows open through openPathAt — the pipeline the terminal's file:line
// links use.

// AgentTraceToggleMsg runs agent.trace.toggle.
type AgentTraceToggleMsg struct{}

// traceTarget is the terminal the trace follows: its session key ("" when no
// terminal qualifies) and the working directory the session is looked up by,
// plus — for the pane header only (#2857) — its name and why it was picked.
type traceTarget struct {
	key  string
	cwd  string
	name string
	why  string
}

// sameTerminal reports whether t follows the same terminal and directory as
// o. The name and the reason never count: focus moving from the agent pane
// to the editor turns "focused" into "last focused", and relocating on that
// would restart a perfectly good read.
func (t traceTarget) sameTerminal(o traceTarget) bool { return t.key == o.key && t.cwd == o.cwd }

// label is the header's "follows" segment.
func (t traceTarget) label() string {
	if t.name == "" {
		return t.why
	}
	return t.name + " (" + t.why + ")"
}

// traceLocatedMsg is the off-loop session lookup's verdict.
type traceLocatedMsg struct {
	gen    int64
	target traceTarget
	sess   agentSession
	err    error
}

// traceReadMsg is one finished incremental read: the regrouped tree, the
// session facts for the header, how many events the read added and the
// reader's revision after it. reader names the Reader that ran it — a read
// of the current reader is applied even when a relocation happened in the
// meantime, since its events are consumed either way (#2857).
type traceReadMsg struct {
	reader *agenttrace.Reader
	nodes  []agenttrace.Node
	info   tracepanel.Info
	added  int
	rev    int
	at     time.Time
	err    error
}

// traceTickMsg drives the poll while the pane is open. gen retires the
// chain of a superseded arming.
type traceTickMsg struct{ gen int64 }

// agentTraceInterval paces the incremental reads: Claude Code appends a line
// per event, and a second is well inside what feels live in a tree.
const agentTraceInterval = time.Second

// agentTraceRescanTicks is how many ticks pass between session lookups while
// no session is found or the followed one has ended — a directory listing
// plus header reads, not something to do every second.
const agentTraceRescanTicks = 5

// agentTraceTickLost is how long without an armed tick counts as a dead
// poll chain (#2857): a tick dropped while the pane was out of reach
// (another workspace) or by a recovered panic ends the chain, and the next
// relocation, read or toggle starts a fresh one.
const agentTraceTickLost = 3 * agentTraceInterval

// agentTraceReadLost is how long a read may stay in flight before the next
// one stops waiting for it: a read whose command panicked never reports
// back, and its busy flag would otherwise freeze the pane for good. The
// Reader serializes the two, so giving up on it is safe.
const agentTraceReadLost = 10 * time.Second

// toggleAgentTracePanel is the agent.trace.toggle state machine: no pane →
// open and locate; open but unfocused → focus it and re-locate (the agent
// pane may have changed); focused → return focus.
func (m *Model) toggleAgentTracePanel() tea.Cmd {
	return m.togglePanelWith(pane.AgentTraceKey, m.openAgentTracePanel, m.traceRelocateCmd)
}

// agentTracePanel returns the singleton panel model, or nil when closed.
func (m Model) agentTracePanel() *tracepanel.Model {
	if inst := m.toolWindow(pane.KindAgentTrace); inst != nil {
		return inst.AgentTrace()
	}
	return nil
}

// openAgentTracePanel splits the active editor with the pane, starts the
// lookup and arms the poll.
func (m *Model) openAgentTracePanel() tea.Cmd {
	if !m.openToolPane(m.activeWS().Panes.AddAgentTrace, m.auxZone, func(key string) {
		p := m.activeWS().Panes.Get(key).AgentTrace()
		p.SetDisplayPath(displayPath)
		p.SetLoading(true)
	}) {
		return nil
	}
	m.traceGen++
	return tea.Batch(m.traceLocateCmd(), m.armTraceTick(true))
}

// traceInitCmd starts the lookup and the poll for a pane restored with the
// session layout; nil when the pane is not part of the layout. Called from
// Init, whose receiver copy is discarded, so it neither bumps a generation
// nor records the followed target — the first tick catches up.
func (m Model) traceInitCmd() tea.Cmd {
	if m.agentTracePanel() == nil {
		return nil
	}
	return tea.Batch(m.traceLocateCmd(), m.armTraceTick(false))
}

// traceTargetNow picks the terminal the trace follows: the focused pane's
// tool terminal, else the most recently focused tool terminal while it is
// still live, else the best of every live terminal — one with a live hook
// binding first, then any tool pane, then a plain terminal — else no
// terminal and the project root.
func (m Model) traceTargetNow() traceTarget {
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil {
		if t := inst.ActiveTerminal(); t != nil && t.Tool() != "" && t.SessionKey() != "" {
			return traceTarget{key: t.SessionKey(), cwd: t.Cwd(), name: t.Tool(), why: "focused"}
		}
	}
	terms := m.agentTerminals()
	if m.recentToolTerm != "" {
		for _, t := range terms {
			if t.key == m.recentToolTerm {
				return traceTarget{key: t.key, cwd: t.cwd, name: t.label(), why: "last focused"}
			}
		}
	}
	best, bestRank := traceTarget{}, 0
	for _, t := range terms {
		rank, why := 1, "only terminal"
		if bound, ok := m.agentSessions[t.key]; ok && !bound.Ended {
			rank, why = 3, "hook-bound"
		} else if t.tool != "" {
			rank, why = 2, "tool pane"
		}
		if rank > bestRank {
			best, bestRank = traceTarget{key: t.key, cwd: t.cwd, name: t.label(), why: why}, rank
		}
	}
	if bestRank > 0 {
		return best
	}
	return traceTarget{cwd: projectRoot(), why: "project root"}
}

// traceLocateCmd looks the followed terminal's session up off the loop:
// the hook binding when there is a live one, else discovery on its cwd.
func (m *Model) traceLocateCmd() tea.Cmd {
	target := m.traceTargetNow()
	m.traceFollow = target
	if p := m.agentTracePanel(); p != nil {
		p.SetFollowing(target.label())
	}
	bound, ok := m.agentSessions[target.key]
	projects := agenttrace.ProjectsDir()
	gen := m.traceGen
	// The forks agent.ask spawned (#2845) are never the traced session.
	forks := append([]string(nil), m.askForks...)
	return func() tea.Msg {
		s, err := locateAgentSession(bound, ok && target.key != "", projects, target.cwd, forks...)
		return traceLocatedMsg{gen: gen, target: target, sess: s, err: err}
	}
}

// handleTraceLocated switches the reader onto the located transcript and
// reads it; a miss shows the pane's empty state.
func (m Model) handleTraceLocated(msg traceLocatedMsg) (tea.Model, tea.Cmd) {
	p := m.agentTracePanel()
	if msg.gen != m.traceGen || p == nil {
		return m, nil
	}
	if msg.err != nil {
		m.traceReader = nil
		m.traceSession = agentSession{}
		p.SetNoSession(msg.target.cwd, msg.err)
		return m, nil
	}
	if m.traceReader == nil || m.traceReader.Path() != msg.sess.Transcript {
		m.traceReader = agenttrace.NewReader(msg.sess.Transcript)
		m.traceBusy, m.tracePending = false, false
		m.traceShownRev = -1
		p.Reset()
	}
	m.traceSession = msg.sess
	return m, tea.Batch(m.traceReadCmd(), m.ensureTraceTick())
}

// traceReadCmd runs one incremental read and regroups the tree off the
// loop. At most one read is in flight; a read asked for meanwhile (a hook
// push, a relocation) is remembered and runs as soon as the current one
// reports, so its lines are not left for the next tick. A read that never
// reported (its command panicked) stops blocking after agentTraceReadLost.
func (m *Model) traceReadCmd() tea.Cmd {
	r := m.traceReader
	if r == nil {
		return nil
	}
	if m.traceBusy && time.Since(m.traceBusyAt) < agentTraceReadLost {
		m.tracePending = true
		return nil
	}
	m.traceBusy, m.traceBusyAt, m.tracePending = true, time.Now(), false
	sess := m.traceSession
	return func() tea.Msg {
		var nodes []agenttrace.Node
		var info tracepanel.Info
		added, rev, err := r.Read(func(s *agenttrace.Session) {
			info = tracepanel.Info{
				ID: s.ID, Transcript: r.Path(), CWD: s.CWD,
				FromHook: sess.FromHook, Ended: sess.Ended, Turns: s.Turns(),
			}
			nodes = agenttrace.BuildTree(s)
		})
		if info.ID == "" {
			info.ID = sess.ID
		}
		if info.CWD == "" {
			info.CWD = sess.CWD
		}
		return traceReadMsg{reader: r, nodes: nodes, info: info, added: added, rev: rev, at: time.Now(), err: err}
	}
}

// handleTraceRead feeds a finished read into the pane. A read of a reader
// that has since been replaced is dropped; any other is applied, and the
// tree is rebuilt whenever the reader's revision differs from the one shown
// — not only when this very read added events: a read whose result was
// dropped, or a completion that changed a row in place, must still reach
// the pane (#2857). Every read stamps the header's liveness segment.
func (m Model) handleTraceRead(msg traceReadMsg) (tea.Model, tea.Cmd) {
	if msg.reader != m.traceReader {
		return m, nil
	}
	m.traceBusy = false
	p := m.agentTracePanel()
	if p == nil {
		return m, nil
	}
	if msg.err != nil {
		m.host.Notify(host.Error, "agent trace: "+msg.err.Error())
		return m, nil
	}
	p.SetRead(msg.at, msg.added)
	if msg.rev != m.traceShownRev || !p.HasSession() || msg.info != p.Info() {
		p.Set(msg.nodes, msg.info)
		m.traceShownRev = msg.rev
	}
	m.syncTraceLinks()
	var again tea.Cmd
	if m.tracePending {
		again = m.traceReadCmd()
	}
	return m, tea.Batch(again, m.ensureTraceTick())
}

// armTraceTick schedules the next poll. bump starts a fresh chain (open),
// retiring any older one; the handler re-arms without bumping.
func (m *Model) armTraceTick(bump bool) tea.Cmd {
	if bump {
		m.traceTickGen++
	}
	m.traceTickAt = time.Now()
	gen := m.traceTickGen
	return tea.Tick(agentTraceInterval, func(time.Time) tea.Msg { return traceTickMsg{gen: gen} })
}

// ensureTraceTick restarts the poll when its chain died (#2857): nil while
// the pane is closed or a tick was armed recently, else a fresh chain that
// retires whatever is left of the old one.
func (m *Model) ensureTraceTick() tea.Cmd {
	if m.agentTracePanel() == nil || time.Since(m.traceTickAt) < agentTraceTickLost {
		return nil
	}
	return m.armTraceTick(true)
}

// handleTraceTick reads the transcript again, re-locates the session when
// the followed terminal changed (or, every few ticks, while nothing is found
// or the session ended) and re-arms while the pane is open.
func (m Model) handleTraceTick(msg traceTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.traceTickGen || m.agentTracePanel() == nil {
		return m, nil
	}
	m.traceTicks++
	target := m.traceTargetNow()
	rescan := m.traceTicks%agentTraceRescanTicks == 0 && (m.traceReader == nil || m.traceSession.Ended)
	var work tea.Cmd
	if !target.sameTerminal(m.traceFollow) || rescan {
		m.traceGen++
		work = m.traceLocateCmd()
	} else {
		m.traceFollow = target
		m.agentTracePanel().SetFollowing(target.label())
		work = m.traceReadCmd()
	}
	return m, tea.Batch(work, m.armTraceTick(false))
}

// traceRelocateCmd is the "look again now" the hook push, the pane's 'r',
// the toggle's re-focus and a finished hook install share; nil while the
// pane is closed. It also revives a poll chain that died (#2857).
func (m *Model) traceRelocateCmd() tea.Cmd {
	if m.agentTracePanel() == nil {
		return nil
	}
	m.traceGen++
	return tea.Batch(m.traceLocateCmd(), m.ensureTraceTick())
}

// noteToolFocus remembers the tool terminal the keyboard last sat in, so a
// trace opened from an editor still follows the agent pane the user came
// from (setFocus).
func (m *Model) noteToolFocus(inst *pane.Instance) {
	if inst == nil {
		return
	}
	if t := inst.ActiveTerminal(); t != nil && t.Tool() != "" && t.SessionKey() != "" {
		m.recentToolTerm = t.SessionKey()
	}
}

// Change-feed linking (#2838): the trace's writing file nodes resolve to the
// change-feed entries they caused (agenttrace.Link — path, the tool call's
// time window and the terminal the trace follows as the source process).
// Linked rows answer D with the feed's mini-diff and V with its revert, and
// the feed jumps back to the node with t. The links are recomputed on every
// read, i.e. once per poll tick, so a write the watcher records after the
// transcript line landed links within a second.

// traceChanges converts the feed into the facts matching reads.
func (m Model) traceChanges() []agenttrace.Change {
	entries := m.feed.Entries()
	out := make([]agenttrace.Change, 0, len(entries))
	for _, e := range entries {
		out = append(out, agenttrace.Change{Path: e.Path, First: e.First, Last: e.Time, SourceKey: e.SourceKey})
	}
	return out
}

// syncTraceLinks relinks the shown tree against the feed and applies a
// pending jump from the feed once the tree is there.
func (m *Model) syncTraceLinks() {
	p := m.agentTracePanel()
	if p == nil || !p.HasSession() {
		return
	}
	links := agenttrace.Link(p.Nodes(), m.traceChanges(), m.traceFollow.key, p.Info().CWD)
	p.SetLinks(links)
	m.traceLinks = links
	if key := m.traceJump; key != "" {
		m.traceJump = ""
		if !p.Select(key) {
			m.host.Notify(host.Info, traceNodeGone)
		}
	}
}

// traceNodeGone is the notice for a back-link whose node the pane does not
// show (the pane now follows another session).
const traceNodeGone = "agent trace: the node behind that change is not in the shown session"

// traceChangeEntry resolves a linked node's path to its live feed entry.
func (m Model) traceChangeEntry(path string) (changefeed.Entry, bool) {
	e, ok := m.feed.Get(path)
	if !ok {
		m.host.Notify(host.Info, "change feed no longer lists "+displayPath(path))
	}
	return e, ok
}

// jumpToTraceNode is the feed's back-link: reveal the trace node that caused
// the entry's newest write. An open pane is focused and selects it right
// away; a closed one opens and selects it once the first read lands.
func (m *Model) jumpToTraceNode(key string) tea.Cmd {
	if p := m.agentTracePanel(); p != nil && p.HasSession() {
		cmd := m.showPanel(pane.AgentTraceKey, m.openAgentTracePanel)
		if !p.Select(key) {
			m.host.Notify(host.Info, traceNodeGone)
		}
		return cmd
	}
	m.traceJump = key
	return m.showPanel(pane.AgentTraceKey, m.openAgentTracePanel)
}

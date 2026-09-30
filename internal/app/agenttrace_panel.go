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
// terminal qualifies) and the working directory the session is looked up by.
type traceTarget struct {
	key string
	cwd string
}

// traceLocatedMsg is the off-loop session lookup's verdict.
type traceLocatedMsg struct {
	gen    int64
	target traceTarget
	sess   agentSession
	err    error
}

// traceReadMsg is one finished incremental read: the regrouped tree, the
// session facts for the header and how many events the read added.
type traceReadMsg struct {
	gen   int64
	nodes []agenttrace.Node
	info  tracepanel.Info
	added int
	err   error
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

// toggleAgentTracePanel is the agent.trace.toggle state machine: no pane →
// open and locate; open but unfocused → focus it and re-locate (the agent
// pane may have changed); focused → return focus.
func (m *Model) toggleAgentTracePanel() tea.Cmd {
	return m.togglePanelWith(pane.AgentTraceKey, m.openAgentTracePanel, func() tea.Cmd {
		m.traceGen++
		return m.traceLocateCmd()
	})
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
			return traceTarget{key: t.SessionKey(), cwd: t.Cwd()}
		}
	}
	terms := m.agentTerminals()
	if m.recentToolTerm != "" {
		for _, t := range terms {
			if t.key == m.recentToolTerm {
				return traceTarget{key: t.key, cwd: t.cwd}
			}
		}
	}
	best, bestRank := traceTarget{}, 0
	for _, t := range terms {
		rank := 1
		if bound, ok := m.agentSessions[t.key]; ok && !bound.Ended {
			rank = 3
		} else if t.tool != "" {
			rank = 2
		}
		if rank > bestRank {
			best, bestRank = traceTarget{key: t.key, cwd: t.cwd}, rank
		}
	}
	if bestRank > 0 {
		return best
	}
	return traceTarget{cwd: projectRoot()}
}

// traceLocateCmd looks the followed terminal's session up off the loop:
// the hook binding when there is a live one, else discovery on its cwd.
func (m *Model) traceLocateCmd() tea.Cmd {
	target := m.traceTargetNow()
	m.traceFollow = target
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
		m.traceBusy = false
		p.Reset()
	}
	m.traceSession = msg.sess
	return m, m.traceReadCmd()
}

// traceReadCmd runs one incremental read and regroups the tree off the
// loop. At most one read is in flight: the reader's session is touched by
// nothing else while it runs.
func (m *Model) traceReadCmd() tea.Cmd {
	r := m.traceReader
	if r == nil || m.traceBusy {
		return nil
	}
	m.traceBusy = true
	gen := m.traceGen
	sess := m.traceSession
	return func() tea.Msg {
		added, err := r.Update()
		s := r.Session()
		info := tracepanel.Info{
			ID: s.ID, Transcript: r.Path(), CWD: s.CWD,
			FromHook: sess.FromHook, Ended: sess.Ended, Turns: s.Turns(),
		}
		if info.ID == "" {
			info.ID = sess.ID
		}
		if info.CWD == "" {
			info.CWD = sess.CWD
		}
		return traceReadMsg{gen: gen, nodes: agenttrace.BuildTree(s), info: info, added: added, err: err}
	}
}

// handleTraceRead feeds a finished read into the pane. Only a read that
// added events (or the first one) rebuilds the tree.
func (m Model) handleTraceRead(msg traceReadMsg) (tea.Model, tea.Cmd) {
	m.traceBusy = false
	p := m.agentTracePanel()
	if msg.gen != m.traceGen || p == nil {
		return m, nil
	}
	if msg.err != nil {
		m.host.Notify(host.Error, "agent trace: "+msg.err.Error())
		return m, nil
	}
	if msg.added > 0 || !p.HasSession() {
		p.Set(msg.nodes, msg.info)
	}
	m.syncTraceLinks()
	return m, nil
}

// armTraceTick schedules the next poll. bump starts a fresh chain (open),
// retiring any older one; the handler re-arms without bumping.
func (m *Model) armTraceTick(bump bool) tea.Cmd {
	if bump {
		m.traceTickGen++
	}
	gen := m.traceTickGen
	return tea.Tick(agentTraceInterval, func(time.Time) tea.Msg { return traceTickMsg{gen: gen} })
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
	if target != m.traceFollow || rescan {
		m.traceGen++
		work = m.traceLocateCmd()
	} else {
		work = m.traceReadCmd()
	}
	return m, tea.Batch(work, m.armTraceTick(false))
}

// traceRelocateCmd is the "look again now" the hook push, the pane's 'r'
// and a finished hook install share; nil while the pane is closed.
func (m *Model) traceRelocateCmd() tea.Cmd {
	if m.agentTracePanel() == nil {
		return nil
	}
	m.traceGen++
	return m.traceLocateCmd()
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

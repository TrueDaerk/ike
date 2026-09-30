package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agenttrace"
	"ike/internal/deeplink"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/project"
	"ike/internal/terminal"
)

// agenthooks.go is the app side of the agent hook push (#2843, epic 0540):
// Claude Code runs `ike agent-hook <event>` on SessionStart / SessionEnd /
// UserPromptSubmit, the CLI forwards the session over the deeplink socket as
// an "event" message, and this file binds that session to the terminal it
// runs in — so the trace tool window (#2840) knows a pane's session without
// scanning ~/.claude/projects. Without a binding it falls back to discovery
// (agenttrace.Discover on the pane's cwd).
//
// It also owns the agent.hooks.install / agent.hooks.uninstall commands that
// write IKE's entries into Claude Code's settings.json.

// AgentEventMsg carries one validated agent lifecycle push into Update.
type AgentEventMsg struct{ Event deeplink.Event }

// AgentHooksInstallMsg / AgentHooksUninstallMsg run the two commands.
type AgentHooksInstallMsg struct{}
type AgentHooksUninstallMsg struct{}

// agentHooksDoneMsg is the off-loop settings edit's verdict.
type agentHooksDoneMsg struct {
	install bool
	path    string
	changed bool
	err     error
}

// agentSession is what IKE knows about the agent session in one terminal.
type agentSession struct {
	ID         string
	Transcript string
	CWD        string
	// Event is the last lifecycle event seen; Ended is set by SessionEnd.
	Event string
	Ended bool
	At    time.Time
	// FromHook is false for a session found by discovery.
	FromHook bool
}

// handleAgentEvent binds the event's session to its terminal. Unmatched
// events are dropped: the pane then falls back to discovery.
func (m Model) handleAgentEvent(ev deeplink.Event) (tea.Model, tea.Cmd) {
	key := m.agentEventTarget(ev)
	if key == "" {
		return m, nil
	}
	if m.agentSessions == nil {
		m.agentSessions = map[string]agentSession{}
	}
	s := agentSession{
		ID: ev.SessionID, Transcript: ev.TranscriptPath, CWD: ev.CWD,
		Event: ev.Event, At: time.Now(), FromHook: true,
	}
	if prev, ok := m.agentSessions[key]; ok && prev.ID == s.ID && s.Transcript == "" {
		s.Transcript = prev.Transcript
	}
	s.Ended = ev.Event == "SessionEnd"
	m.agentSessions[key] = s
	return m, nil
}

// agentTerm is one terminal an agent may run in.
type agentTerm struct {
	key  string // terminal session key ($IKE_SESSION)
	tool string // custom tool name, "" for a plain terminal
	cwd  string
}

// agentTerminals lists every live terminal of this instance: panes and
// editor-tab terminals of the active and every parked workspace, plus the
// parked global tools.
func (m Model) agentTerminals() []agentTerm {
	var out []agentTerm
	add := func(t *terminal.Model) {
		if t == nil || t.SessionKey() == "" {
			return
		}
		out = append(out, agentTerm{key: t.SessionKey(), tool: t.Tool(), cwd: t.Cwd()})
	}
	scan := func(reg *pane.Registry) {
		if reg == nil {
			return
		}
		for _, k := range reg.Keys() {
			inst := reg.Get(k)
			if inst == nil {
				continue
			}
			switch inst.Kind() {
			case pane.KindTerminal:
				add(inst.Terminal())
			case pane.KindEditor:
				for i := 0; i < inst.TabCount(); i++ {
					add(inst.TabTerminal(i))
				}
			}
		}
	}
	if ws := m.activeWS(); ws != nil {
		scan(ws.Panes)
	}
	for _, root := range m.ws.Background() {
		if ws := m.ws.Peek(root); ws != nil {
			scan(ws.Panes)
		}
	}
	for _, name := range m.ws.GlobalToolNames() {
		if t, ok := m.ws.PeekGlobalTool(name); ok {
			add(&t)
		}
	}
	return out
}

// agentEventTarget picks the terminal an event belongs to: the one named by
// $IKE_SESSION when the event comes from this instance, else a terminal
// whose cwd is the event's — preferring one already bound to the session,
// then a tool pane, then an unbound terminal. "" when nothing matches.
func (m Model) agentEventTarget(ev deeplink.Event) string {
	terms := m.agentTerminals()
	if ev.Pane != "" && ev.PID == os.Getpid() {
		for _, t := range terms {
			if t.key == ev.Pane {
				return t.key
			}
		}
	}
	best, bestRank := "", 0
	for _, t := range terms {
		if !agentSameDir(t.cwd, ev.CWD) {
			continue
		}
		rank := 1
		bound, ok := m.agentSessions[t.key]
		switch {
		case ok && bound.ID == ev.SessionID:
			rank = 4
		case t.tool != "" && (!ok || bound.Ended):
			rank = 3
		case !ok || bound.Ended:
			rank = 2
		}
		if rank > bestRank {
			best, bestRank = t.key, rank
		}
	}
	return best
}

// agentSameDir compares two directories, tolerating symlinks (macOS reports
// /private/tmp for /tmp) and a trailing slash.
func agentSameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return a == b
}

// agentSessionFor returns the hook-bound session of a terminal.
func (m Model) agentSessionFor(sessionKey string) (agentSession, bool) {
	s, ok := m.agentSessions[sessionKey]
	return s, ok
}

// agentSessionLocator returns a function — to run off the Update loop — that
// yields the agent session of terminal t: the hook binding when there is a
// live one, else the newest session discovery finds for the terminal's cwd.
func (m Model) agentSessionLocator(t *terminal.Model) func() (agentSession, error) {
	bound, ok := m.agentSessionFor(t.SessionKey())
	cwd := t.Cwd()
	projects := agenttrace.ProjectsDir()
	return func() (agentSession, error) {
		return locateAgentSession(bound, ok, projects, cwd)
	}
}

// locateAgentSession is agentSessionLocator's body with its inputs explicit.
func locateAgentSession(bound agentSession, ok bool, projectsDir, cwd string) (agentSession, error) {
	if ok && !bound.Ended {
		return bound, nil
	}
	l, err := agenttrace.DiscoverIn(projectsDir, cwd)
	if err != nil {
		return agentSession{}, err
	}
	return agentSession{ID: l.ID, Transcript: l.Path, CWD: l.CWD, At: l.ModTime}, nil
}

// agentHookExe is the binary the hooks call; tests swap it (the test binary
// is not named ike).
var agentHookExe = os.Executable

// agentHooksCmd runs the settings edit off the loop.
func agentHooksCmd(install bool) tea.Cmd {
	return func() tea.Msg {
		path := agenttrace.SettingsPath()
		if install {
			exe, err := agentHookExe()
			if err == nil {
				exe, err = filepath.EvalSymlinks(exe)
			}
			if err != nil {
				return agentHooksDoneMsg{install: true, path: path, err: err}
			}
			changed, err := agenttrace.InstallHooks(path, exe)
			return agentHooksDoneMsg{install: true, path: path, changed: changed, err: err}
		}
		changed, err := agenttrace.UninstallHooks(path)
		return agentHooksDoneMsg{path: path, changed: changed, err: err}
	}
}

// handleAgentHooksDone reports the edit.
func (m Model) handleAgentHooksDone(msg agentHooksDoneMsg) (tea.Model, tea.Cmd) {
	where := project.CompactPath(msg.path)
	switch {
	case msg.err != nil:
		verb := "uninstall"
		if msg.install {
			verb = "install"
		}
		m.host.Notify(host.Error, "Claude hooks: "+verb+" failed: "+msg.err.Error())
	case msg.install && msg.changed:
		m.host.Notify(host.Info, "Claude hooks installed in "+where+" ("+strings.Join(agenttrace.HookEvents, ", ")+")")
	case msg.install:
		m.host.Notify(host.Info, "Claude hooks already up to date in "+where)
	case msg.changed:
		m.host.Notify(host.Info, "Claude hooks removed from "+where)
	default:
		m.host.Notify(host.Info, "no IKE Claude hooks in "+where)
	}
	return m, nil
}

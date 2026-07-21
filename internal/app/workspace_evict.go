package app

import (
	"ike/internal/config"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/project"
	"ike/internal/ui"
	"ike/internal/workspace"

	tea "charm.land/bubbletea/v2"
)

// workspace_evict.go bounds the background workspace set (0370 M4, #780):
// after every seamless switch the manager is held to project.max_workspaces
// parked workspaces. The least-recently-used one is evicted — silently when
// it is idle, behind a confirm prompt when unsaved buffers or running
// processes would die (the 0090 unsaved-changes guard reborn at eviction
// time; plain switching itself never prompts since #777).

// defaultMaxWorkspaces is the background cap when project.max_workspaces is
// unset or invalid.
const defaultMaxWorkspaces = 3

// maxWorkspaces reads the configured background cap, floored at 1.
func maxWorkspaces() int {
	c := config.Get()
	if c == nil || c.Project.MaxWorkspaces <= 0 {
		return defaultMaxWorkspaces
	}
	return c.Project.MaxWorkspaces
}

// workspaceBusy reports whether evicting w would lose live state: a dirty
// editor buffer, a running terminal (shell, tool or command session,
// including terminal tabs), or a parked debug session.
func workspaceBusy(w *workspace.Workspace) bool {
	if w == nil {
		return false
	}
	for _, key := range w.Panes.Keys() {
		inst := w.Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			if inst.Terminal().Running() {
				return true
			}
		case pane.KindEditor:
			for i := 0; i < inst.TabCount(); i++ {
				if ed := inst.TabEditor(i); ed != nil && ed.Dirty() {
					return true
				}
				if t := inst.TabTerminal(i); t != nil && t.Running() {
					return true
				}
			}
		}
	}
	if extras, ok := w.Aux.(wsExtras); ok && extras.dbg != nil && extras.dbg.sess != nil {
		return true
	}
	return false
}

// teardownWorkspace releases a dropped workspace's live resources: every
// terminal session (panes and tabs) closes and a parked debug session
// disconnects. Buffers need no teardown — dropping the registry is enough.
// The pane registry and split tree are cut loose at the end so a Workspace
// pointer lingering anywhere cannot pin them (#825).
func teardownWorkspace(w *workspace.Workspace) {
	if w == nil {
		return
	}
	for _, key := range w.Panes.Keys() {
		inst := w.Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			inst.Terminal().Close()
		case pane.KindEditor:
			inst.CloseTerminalTabs()
		}
	}
	if extras, ok := w.Aux.(wsExtras); ok && extras.dbg != nil && extras.dbg.sess != nil {
		sess := extras.dbg.sess
		_ = sess.Disconnect()
		go sess.Close()
	}
	w.Aux = nil
	w.Panes = nil
	w.Tree = nil
	w.ReturnFocus = ""
}

// closeWorkspace tears w down and fires the workspace-closed hooks (#825), so
// subscribers — the LSP bridge, notably — release everything they hold under
// the workspace's root. The returned cmd carries the hooks' async work.
func (m Model) closeWorkspace(w *workspace.Workspace) tea.Cmd {
	if w == nil {
		return nil
	}
	root := w.Root
	teardownWorkspace(w)
	return tea.Batch(m.fireHooks(plugin.EventWorkspaceClosed, root)...)
}

// enforceWorkspaceCap evicts least-recently-used background workspaces past
// the cap: idle ones silently, the first busy one behind the confirm prompt
// (one decision at a time; the cap re-checks after the next switch). The
// returned cmd carries the evicted workspaces' close hooks (#825).
func (m *Model) enforceWorkspaceCap() tea.Cmd {
	cap := maxWorkspaces()
	var cmds []tea.Cmd
	for {
		bg := m.ws.Background()
		if len(bg) <= cap {
			return tea.Batch(cmds...)
		}
		lru := bg[0]
		if workspaceBusy(m.ws.Peek(lru)) {
			m.openEvictPrompt(lru)
			return tea.Batch(cmds...)
		}
		if cmd := m.closeWorkspace(m.ws.Drop(lru)); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
}

// openEvictPrompt shows the eviction guard for the busy LRU workspace at
// root: e evicts (killing its processes and discarding unsaved changes), esc
// keeps it (the cap stays exceeded until the next switch re-asks).
func (m *Model) openEvictPrompt(root string) {
	m.evictPending = root
	m.shell.SetContent(ui.ModelContent{
		Heading: "Background workspace limit",
		Body: func() string {
			return "the background workspace\n" +
				project.CompactPath(root) + "\nstill has unsaved changes or running processes\n" +
				"(limit project.max_workspaces exceeded).\n\n" +
				"  [e]   evict it — stop its processes, discard unsaved changes\n" +
				"  [esc] keep it running (over the limit, asked again next switch)"
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// evictPromptOpen reports whether the shell currently shows the guard.
func (m Model) evictPromptOpen() bool { return m.evictPending != "" && m.shell.IsOpen() }

// updateEvictPrompt consumes every key while the guard is open.
func (m Model) updateEvictPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "e":
		root := m.evictPending
		m.evictPending = ""
		m.shell.Close()
		closeCmd := m.closeWorkspace(m.ws.Drop(root))
		capCmd := m.enforceWorkspaceCap() // more may be over the cap
		return m, tea.Batch(closeCmd, capCmd)
	case "esc":
		m.evictPending = ""
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

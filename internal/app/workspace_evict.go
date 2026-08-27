package app

import (
	"runtime/debug"

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
	if extras, ok := w.Aux.(wsExtras); ok {
		if extras.dbg != nil && extras.dbg.sess != nil {
			return true
		}
		// A parked popup terminal (#1407) or project-owned floating panel
		// (#1793) with a running session dies with the workspace — ask first,
		// like pane terminals. Global panels never park, so they never count.
		for _, inst := range parkedPopupInstances(extras) {
			for i := 0; i < inst.TabCount(); i++ {
				if t := inst.TabTerminal(i); t != nil && t.Running() {
					return true
				}
			}
		}
	}
	return false
}

// setWorkspaceTerminalsParked flips the parked flag (#1522) on every terminal
// session a workspace holds — terminal panes, editor terminal tabs, and the
// parked popup terminal riding in Aux. Parked sessions keep ingesting PTY
// output into their grid but stop requesting repaints and batch their feed;
// un-parking delivers the one owed repaint per session.
func setWorkspaceTerminalsParked(w *workspace.Workspace, parked bool) {
	if w == nil || w.Panes == nil {
		return
	}
	for _, key := range w.Panes.Keys() {
		inst := w.Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			inst.Terminal().SetParked(parked)
		case pane.KindEditor:
			for i := 0; i < inst.TabCount(); i++ {
				if t := inst.TabTerminal(i); t != nil {
					t.SetParked(parked)
				}
			}
		case pane.KindDebug:
			// The debug area's embedded console (#2190) parks like any
			// terminal session: its pipe/PTY feed batches while backgrounded.
			if t := inst.Debug().Term(); t != nil {
				t.SetParked(parked)
			}
		}
	}
	if extras, ok := w.Aux.(wsExtras); ok {
		for _, inst := range parkedPopupInstances(extras) {
			for i := 0; i < inst.TabCount(); i++ {
				if t := inst.TabTerminal(i); t != nil {
					t.SetParked(parked)
				}
			}
		}
	}
}

// parkedPopupInstances returns every popup-layer tab host riding in a parked
// workspace's Aux: the popup box's sides (#1407) plus the project-owned
// floating panels (#1793). Global panels never enter Aux — they stay with the
// live model and survive the workspace's whole lifecycle.
func parkedPopupInstances(extras wsExtras) []*pane.Instance {
	out := extras.popup.instances()
	for _, f := range extras.floats {
		out = append(out, f.inst)
	}
	return out
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
		case pane.KindDebug:
			inst.Debug().CloseTerm() // the embedded console session (#2190)
		}
	}
	if extras, ok := w.Aux.(wsExtras); ok {
		if extras.dbg != nil && extras.dbg.sess != nil {
			sess := extras.dbg.sess
			_ = sess.Disconnect()
			go sess.Close()
		}
		for _, inst := range parkedPopupInstances(extras) {
			// Parked popup terminal and floating-panel shells (#1407, #1793)
			// end tidily, like the quit path. Global panels are not in Aux —
			// a workspace teardown never reaps them (#1793 lifecycle rule).
			inst.CloseTerminalTabs()
		}
	}
	w.Aux = nil
	w.Panes = nil
	w.Tree = nil
	w.ReturnFocus = ""
	// A workspace teardown frees the largest chunks this process ever lets go
	// of (buffers, scrollbacks, undo trees). Hand the pages back to the OS —
	// on macOS freed-but-retained pages otherwise stay counted against the
	// process footprint until memory pressure (#1537). Async: FreeOSMemory
	// forces a full GC and can take tens of milliseconds.
	go debug.FreeOSMemory()
}

// closeWorkspace tears w down and fires the workspace-closed hooks (#825), so
// subscribers — the LSP bridge, notably — release everything they hold under
// the workspace's root. The returned cmd carries the hooks' async work.
func (m Model) closeWorkspace(w *workspace.Workspace) tea.Cmd {
	if w == nil {
		return nil
	}
	root := w.Root
	// Kitty placements a torn-down workspace still holds leave the terminal
	// (#1547) — normally already released at park time, so this catches the
	// paths that close a workspace without a prior switch.
	imgCmd := m.releaseWorkspaceImages(w)
	// Its crash snapshots leave the disk (#1550): a discard-close already
	// settled (or deliberately dropped) the unsaved changes, so a leftover
	// snapshot would resurface them as a crash-recovery prompt next launch.
	m.backupDropWorkspace(w)
	// Its editor paths stop poll-watching (#1562): the last-view-close
	// untrack (#1541) never fires for panes torn down whole, so their stamps
	// and save epochs would sit in the active model's watcher — re-stat'ed
	// every poll — for the rest of the session. Collected before teardown
	// cuts the registry loose; paths still open elsewhere stay tracked
	// (checked after teardown, so w itself no longer counts as open).
	var paths []string
	for _, key := range w.Panes.Keys() {
		inst := w.Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() {
				paths = append(paths, ed.Path())
			}
		}
	}
	teardownWorkspace(w)
	if m.watcher != nil {
		for _, path := range paths {
			if !m.pathOpenAnywhere(path) {
				m.watcher.Untrack(path)
			}
		}
	}
	return tea.Batch(append(m.fireHooks(plugin.EventWorkspaceClosed, root), imgCmd)...)
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
				guardLine("e", "evict it — stop its processes, discard unsaved changes", true) +
				guardCancel("keep it running (over the limit, asked again next switch)")
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// evictPromptOpen reports whether the shell currently shows the guard.
func (m Model) evictPromptOpen() bool { return m.evictPending != "" && m.shell.IsOpen() }

// updateEvictPrompt consumes every key while the guard is open; enter answers
// for the primary option, e (#1356).
func (m Model) updateEvictPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch guardAnswer(msg, "e") {
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

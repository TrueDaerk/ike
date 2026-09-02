package app

import (
	"runtime/debug"

	"ike/internal/config"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/workspace"

	tea "charm.land/bubbletea/v2"
)

// workspace_evict.go bounds the background workspace set (0370 M4, #780):
// after every seamless switch the manager is held to project.max_workspaces
// parked workspaces. Least-recently-used idle workspaces are evicted
// silently; a busy one (unsaved buffers or running processes) is kept even
// over the cap — since #2396 a switch never prompts at all, and the quit
// guard at exit is the single place that still asks about live state.

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
// the cap — idle ones only, silently. A busy workspace (unsaved buffers,
// running processes, a parked debug session) is skipped and kept even over
// the cap (#2396): a switch never asks, and live state never dies for a
// limit. Every switch re-runs the sweep, so the set shrinks back the moment
// the busy workspaces go idle. The returned cmd carries the evicted
// workspaces' close hooks (#825).
func (m *Model) enforceWorkspaceCap() tea.Cmd {
	cap := maxWorkspaces()
	var cmds []tea.Cmd
	bg := m.ws.Background()
	over := len(bg) - cap
	for _, root := range bg { // LRU-first order
		if over <= 0 {
			break
		}
		if workspaceBusy(m.ws.Peek(root)) {
			continue
		}
		if cmd := m.closeWorkspace(m.ws.Drop(root)); cmd != nil {
			cmds = append(cmds, cmd)
		}
		over--
	}
	return tea.Batch(cmds...)
}

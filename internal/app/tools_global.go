package app

import (
	"ike/internal/config"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/terminal"
	"ike/internal/workspace"
)

// tools_global.go implements global tool instances (#1890): a [[tools.custom]]
// entry with global = true runs as one process-wide instance shared across
// every workspace. The live session's owner is the workspace manager, not any
// workspace registry — on a project switch the session detaches from the
// departing workspace's layout (detachGlobalTools) and parks on the manager,
// and opening the tool anywhere re-attaches the same running session
// (attachGlobalTool). Workspace teardown paths (switch, project close, LRU
// eviction) therefore never see a global tool; only closing its pane or
// quitting IKE ends the process. A global tool's Cwd resolves once, at first
// spawn, against the project active then — the session keeps its working
// directory across projects, which is the point of sharing it.

// globalToolEntry resolves a tool name to its config entry only when the entry
// is marked global; ok=false for non-global and unconfigured names.
func globalToolEntry(name string) (config.ToolEntry, bool) {
	if name == "" {
		return config.ToolEntry{}, false
	}
	entry, ok := toolEntry(name)
	if !ok || !entry.Global {
		return config.ToolEntry{}, false
	}
	return entry, true
}

// detachGlobalTools extracts every global tool session from the active
// workspace and parks it on the manager (#1890). performSwitch runs it right
// after the chdir succeeds, before the workspace parks, so switch, project
// close and a later eviction of the parked workspace can never end the
// session. Dedicated panes leave the split tree and the registry with their
// session intact; editor-hosted tool tabs (a center drop, #708) detach from
// their strip the same way. Parked sessions stop requesting repaints (#1522)
// until they re-attach.
func (m *Model) detachGlobalTools() {
	ws := m.activeWS()
	if ws == nil || ws.Panes == nil {
		return
	}
	for _, key := range ws.Panes.Keys() {
		inst := ws.Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			if entry, ok := globalToolEntry(inst.Terminal().Tool()); ok {
				m.detachGlobalToolPane(ws, key, inst, entry.Name)
			}
		case pane.KindEditor:
			// Walk downward so a removal never shifts a pending index.
			for i := inst.TabCount() - 1; i >= 0; i-- {
				t := inst.TabTerminal(i)
				if t == nil {
					continue
				}
				entry, ok := globalToolEntry(t.Tool())
				if !ok {
					continue
				}
				if inst.TabCount() == 1 {
					// An editor instance never holds zero tabs; a scratch
					// tab takes over the pane (like a pruned debugTerm leaf
					// restores as an empty editor, #1370).
					inst.AddTab()
				}
				if term, ok := inst.DetachTerminalTab(i); ok {
					m.parkGlobalTool(entry.Name, term)
				}
			}
		}
	}
}

// detachGlobalToolPane removes one dedicated global tool pane from the
// workspace without ending its session: the leaf leaves the tree (the sole
// leaf is replaced by a scratch editor instead — the tree can never go empty),
// the session detaches from the instance, and the emptied instance closes
// harmlessly. Focus moves off the vanished pane like a tool toggle would.
func (m *Model) detachGlobalToolPane(ws *workspace.Workspace, key string, inst *pane.Instance, name string) {
	if ws.Tree != nil {
		if tree, ok := layout.Close(ws.Tree, key); ok {
			ws.Tree = tree
		} else {
			ed := ws.Panes.AddEditor()
			tree, ok := layout.Replace(ws.Tree, key, ed)
			if !ok {
				ws.Panes.Close(ed)
				return // cannot free the slot; the tool stays workspace-owned
			}
			ws.Tree = tree
		}
	}
	term, ok := inst.DetachTerminal()
	if !ok {
		return
	}
	ws.Panes.Close(key) // session-less after the detach: harmless
	if ws.Panes.Focused() == "" {
		target := ws.ReturnFocus
		if target == "" || !ws.Panes.Has(target) {
			target = pane.ExplorerKey
		}
		ws.Panes.SetFocused(target)
	}
	m.parkGlobalTool(name, term)
}

// parkGlobalTool stashes a detached global tool session on the manager,
// parked (#1522) so a session no workspace renders stops requesting repaints.
func (m *Model) parkGlobalTool(name string, term terminal.Model) {
	term.SetParked(true)
	m.ws.ParkGlobalTool(name, term)
}

// attachGlobalTool splices the parked global tool session into the active
// workspace as a dedicated pane — the same placement rules as a fresh spawn
// (home position #1889, else the adaptive auxZone), except tab-hosting is
// skipped: a global tool always lives in its own pane, so the next switch can
// detach it wholesale. A failed splice puts the session back on the manager
// instead of dropping it.
func (m *Model) attachGlobalTool(entry config.ToolEntry, term terminal.Model) {
	ws := m.activeWS()
	target := m.activeEditorKey()
	if target == "" {
		target = ws.Panes.Focused()
	}
	if target == "" || ws.Tree == nil {
		m.ws.ParkGlobalTool(entry.Name, term) // nowhere to attach; keep it parked
		return
	}
	term.SetParked(false)
	ws.ReturnFocus = ws.Panes.Focused()
	key := ws.Panes.AddTerminalPaneFrom(term)
	if zone, ok := toolHomeZone(entry.Placement); ok {
		if occupant := m.dockOccupant(zone); occupant != "" {
			// A dock occupant shares the edge via a perpendicular split,
			// like openToolAtHome's non-tabbable branch.
			share := layout.ZoneBottom
			if zone == layout.ZoneTop || zone == layout.ZoneBottom {
				share = layout.ZoneRight
			}
			tree, ok := layout.SplitLeaf(ws.Tree, occupant, key, share)
			if !ok {
				m.reparkGlobalToolPane(entry.Name, key)
				return
			}
			ws.Tree = tree
		} else {
			ws.Tree = layout.DockNew(ws.Tree, key, zone, toolDockShare)
		}
	} else {
		tree, ok := layout.SplitLeaf(ws.Tree, target, key, m.auxZone(target))
		if !ok {
			m.reparkGlobalToolPane(entry.Name, key)
			return
		}
		ws.Tree = tree
	}
	m.setFocus(key)
	m.rememberTool(entry.Name, key)
	m.layout()
	saveLayout(ws.Tree, ws.Panes)
}

// reparkGlobalToolPane undoes a failed attach: the freshly added pane's
// session detaches again and returns to the manager stash.
func (m *Model) reparkGlobalToolPane(name, key string) {
	if inst := m.activeWS().Panes.Get(key); inst != nil {
		if term, ok := inst.DetachTerminal(); ok {
			m.parkGlobalTool(name, term)
		}
	}
	m.activeWS().Panes.Close(key)
}

// reapGlobalTool ends the parked global tool whose process exited while
// detached: its ExitedMsg resolves to no pane, so without this the stash
// would offer a dead session on the next open. Reports whether sess was a
// parked global tool's session.
func (m *Model) reapGlobalTool(sess string) bool {
	for _, name := range m.ws.GlobalToolNames() {
		term, ok := m.ws.TakeGlobalTool(name)
		if !ok {
			continue
		}
		if term.SessionKey() == sess {
			term.Close()
			return true
		}
		m.ws.ParkGlobalTool(name, term) // not this one; put it back
	}
	return false
}

// closeParkedGlobalTools ends every detached global tool session — the quit
// path's counterpart to the active registry's terminal teardown, so no global
// tool process outlives IKE (#1890).
func (m *Model) closeParkedGlobalTools() {
	for _, name := range m.ws.GlobalToolNames() {
		if term, ok := m.ws.TakeGlobalTool(name); ok {
			term.Close()
		}
	}
}

// restoredToolSession returns the terminal session for a tool tab being
// restored from a saved layout: a parked live global instance re-attaches
// (#1890) instead of spawning a duplicate; anything else spawns fresh, like
// every restore always has.
func (m *Model) restoredToolSession(reg *pane.Registry, entry config.ToolEntry) terminal.Model {
	if entry.Global && m.ws != nil {
		if term, ok := m.ws.TakeGlobalTool(entry.Name); ok {
			term.SetParked(false)
			return term
		}
	}
	dir := entry.Cwd
	if dir == "" {
		dir = "."
	}
	argv := append([]string{entry.Command}, entry.Args...)
	return reg.NewToolSession(entry.Name, argv, dir, toolSpawnEnv(m.pal()), m.host.Send)
}

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
// and the switch-in re-attaches the same running session automatically
// (attachOpenGlobalTools, #1903) — the pane follows the user across projects;
// tool.<name> re-attaches it too (attachGlobalTool). Workspace teardown paths
// (switch, project close, LRU eviction) therefore never see a global tool;
// only closing its pane or quitting IKE ends the process, and an explicit
// close is recorded on the manager so stale layout entries in other projects
// cannot resurrect the tool. A session that exits while parked stays stashed
// with its exit status and materializes as the #810 exited overlay on the
// next switch-in. A global tool's Cwd resolves once, at first spawn, against
// the project active then — the session keeps its working directory across
// projects, which is the point of sharing it.

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
// session intact; tab-hosted global sessions (a center drop #708, or a shared
// slot pane #1901) detach as tabs, the host keeping its other tabs — a host
// holding nothing but global tool sessions detaches whole instead, its leaf
// leaving the tree like a dedicated pane's. Parked sessions stop requesting
// repaints (#1522) until they re-attach.
func (m *Model) detachGlobalTools() {
	ws := m.activeWS()
	if ws == nil || ws.Panes == nil {
		return
	}
	m.rememberActiveToolTabs()
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
			if globalOnlyTabHost(inst) {
				// Every tab is a global tool session: nothing would remain
				// worth a pane, so the whole host detaches like a dedicated
				// pane instead of lingering as a scratch editor (#1901).
				m.detachGlobalToolHost(ws, key, inst)
				continue
			}
			// Walk downward so a removal never shifts a pending index. At
			// least one non-global tab stays behind, so the detach never
			// hits DetachTerminalTab's last-tab refusal.
			for i := inst.TabCount() - 1; i >= 0; i-- {
				t := inst.TabTerminal(i)
				if t == nil {
					continue
				}
				entry, ok := globalToolEntry(t.Tool())
				if !ok {
					continue
				}
				if term, ok := inst.DetachTerminalTab(i); ok {
					m.parkGlobalTool(entry.Name, term)
				}
			}
		}
	}
}

// globalOnlyTabHost reports whether the editor-kind pane hosts nothing but
// global tool sessions — the shape a shared slot pane (#1897/#1901) takes
// once its non-global tabs are gone.
func globalOnlyTabHost(inst *pane.Instance) bool {
	n := inst.TabCount()
	if n == 0 {
		return false
	}
	for i := 0; i < n; i++ {
		t := inst.TabTerminal(i)
		if t == nil {
			return false
		}
		if _, ok := globalToolEntry(t.Tool()); !ok {
			return false
		}
	}
	return true
}

// detachGlobalToolPane removes one dedicated global tool pane from the
// workspace without ending its session: the leaf leaves the tree (the sole
// leaf is replaced by a scratch editor instead — the tree can never go empty),
// the session detaches from the instance, and the emptied instance closes
// harmlessly. Focus moves off the vanished pane like a tool toggle would.
func (m *Model) detachGlobalToolPane(ws *workspace.Workspace, key string, inst *pane.Instance, name string) {
	if !freeGlobalToolLeaf(ws, key) {
		return // cannot free the slot; the tool stays workspace-owned
	}
	term, ok := inst.DetachTerminal()
	if !ok {
		return
	}
	ws.Panes.Close(key) // session-less after the detach: harmless
	settleFocusAfterDetach(ws)
	m.parkGlobalTool(name, term)
}

// detachGlobalToolHost removes a tab host whose every tab is a global tool
// session (#1901): the leaf leaves the tree like a dedicated pane's, every
// hosted session parks, and the emptied instance closes harmlessly.
func (m *Model) detachGlobalToolHost(ws *workspace.Workspace, key string, inst *pane.Instance) {
	if !freeGlobalToolLeaf(ws, key) {
		return // cannot free the slot; the tools stay workspace-owned
	}
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
			// An editor instance never holds zero tabs; a scratch tab covers
			// the last detach (#1370 precedent) and closes with the instance
			// right below.
			inst.AddTab()
		}
		if term, ok := inst.DetachTerminalTab(i); ok {
			m.parkGlobalTool(entry.Name, term)
		}
	}
	ws.Panes.Close(key)
	settleFocusAfterDetach(ws)
}

// freeGlobalToolLeaf takes the pane's leaf out of the workspace tree without
// touching the pane itself: the ordinary close collapse, or — for the sole
// leaf — a scratch editor replacement so the tree never goes empty. Reports
// whether the leaf is out.
func freeGlobalToolLeaf(ws *workspace.Workspace, key string) bool {
	if ws.Tree == nil {
		return true
	}
	if tree, ok := layout.Close(ws.Tree, key); ok {
		ws.Tree = tree
		return true
	}
	ed := ws.Panes.AddEditor()
	tree, ok := layout.Replace(ws.Tree, key, ed)
	if !ok {
		ws.Panes.Close(ed)
		return false
	}
	ws.Tree = tree
	return true
}

// settleFocusAfterDetach moves focus off a vanished global tool pane like a
// tool toggle would: the remembered return pane, else the explorer.
func settleFocusAfterDetach(ws *workspace.Workspace) {
	if ws.Panes.Focused() != "" {
		return
	}
	target := ws.ReturnFocus
	if target == "" || !ws.Panes.Has(target) {
		target = pane.ExplorerKey
	}
	ws.Panes.SetFocused(target)
}

// parkGlobalTool stashes a detached global tool session on the manager,
// parked (#1522) so a session no workspace renders stops requesting repaints.
func (m *Model) parkGlobalTool(name string, term terminal.Model) {
	term.SetParked(true)
	m.ws.ParkGlobalTool(name, term)
}

// attachGlobalTool splices the parked global tool session into the active
// workspace — the same placement rules as a fresh spawn: a slot assignment
// (#1897) pins it, an occupied tab-capable slot pane takes the live session
// as a focused tab (#1901, mirroring openToolAtSlot; the next switch extracts
// it tab-wise again); otherwise the home position (#1889) or the adaptive
// auxZone places a dedicated pane — outside its slot a global tool never
// tab-hosts, so the next switch can detach it wholesale. A failed splice puts
// the session back on the manager instead of dropping it.
func (m *Model) attachGlobalTool(entry config.ToolEntry, term terminal.Model) {
	m.attachGlobalToolIn(entry, term, true)
}

// attachGlobalToolIn is attachGlobalTool with the focus move optional: the
// tool.<name> re-attach focuses the arriving pane like a fresh open, while
// the switch-in auto-attach (#1903) leaves focus where the switch put it.
func (m *Model) attachGlobalToolIn(entry config.ToolEntry, term terminal.Model, focus bool) {
	ws := m.activeWS()
	target := m.activeEditorKey()
	if target == "" {
		target = ws.Panes.Focused()
	}
	if target == "" || ws.Tree == nil {
		m.ws.ParkGlobalTool(entry.Name, term) // nowhere to attach; keep it parked
		return
	}
	tpl, slot := slotTemplate(), toolSlot(entry.Name)
	slotted := tpl != nil && slot != "" && tpl.HasSlot(slot)
	if slotted {
		if resident := m.slotResidents()[slot]; resident != "" &&
			canHostTabs(ws.Panes.Get(resident)) && m.ensureTabHost(resident) {
			term.SetParked(false)
			if focus {
				ws.ReturnFocus = ws.Panes.Focused()
			}
			ws.Panes.Get(resident).AddTerminalTab(term)
			if focus {
				m.setFocus(resident)
			}
			m.rememberTool(entry.Name, resident)
			m.layout()
			saveLayout(ws.Tree, ws.Panes)
			return
		}
	}
	term.SetParked(false)
	if focus {
		ws.ReturnFocus = ws.Panes.Focused()
	}
	key := ws.Panes.AddTerminalPaneFrom(term)
	if slotted {
		// A free slot materializes at the template position; a non-tabbable
		// resident (a singleton panel) subdivides (placePaneInSlot).
		if !m.placePaneInSlot(tpl, slot, key) {
			m.reparkGlobalToolPane(entry.Name, key)
			return
		}
	} else if zone, ok := toolHomeZone(entry.Placement); ok {
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
	if focus {
		m.setFocus(key)
	}
	m.rememberTool(entry.Name, key)
	m.layout()
	saveLayout(ws.Tree, ws.Panes)
}

// attachOpenGlobalTools splices every still-parked global tool session into
// the just-activated workspace (#1903): an open global tool is visible in
// every project view instead of sticking to the project it was last opened
// in. performSwitch runs it after the rebuild — a restore that already
// re-attached the session from the saved layout left nothing parked, so no
// duplicate can arise; the resumed-workspace case never holds one (it was
// detached at park time). A session whose process exited while parked
// attaches too and renders the #810 exited overlay. A tool the incoming
// project's config does not declare global stays parked, and focus stays
// where the switch put it.
func (m *Model) attachOpenGlobalTools() {
	if m.ws == nil || m.activeWS() == nil {
		return
	}
	for _, name := range m.ws.GlobalToolNames() {
		entry, ok := globalToolEntry(name)
		if !ok {
			continue // not configured (as global) under this project's config
		}
		if len(m.toolLocations(name)) > 0 {
			continue // an instance is already attached; first one wins
		}
		if term, taken := m.ws.TakeGlobalTool(name); taken {
			m.attachGlobalToolIn(entry, term, false)
		}
	}
}

// rememberActiveToolTabs records every global tool that is its host pane's
// active tab right now (#1906), so the switch back into this project can
// re-select it once the detached sessions re-attach. The record is replaced
// wholesale: a pane that has since moved to a file, a content tab or a
// project-local tool simply contributes nothing, and its plain tab index
// survives the detach on its own.
func (m *Model) rememberActiveToolTabs() {
	ws := m.activeWS()
	ws.ActiveTools = nil
	for _, key := range ws.Panes.Keys() {
		inst := ws.Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		if t := inst.TabTerminal(inst.ActiveTab()); t != nil {
			if entry, ok := globalToolEntry(t.Tool()); ok {
				ws.ActiveTools = append(ws.ActiveTools, entry.Name)
			}
		}
	}
}

// snapshotActiveTabs records every pane's active tab index — the "before"
// half of the switch-in restore (#1906), taken right before the global tool
// attach appends (and activates) its tabs. Terminal panes are in it at index
// 0: an arriving tool converts them into tab hosts (#1901), and their own
// session must stay the selected tab.
func (m *Model) snapshotActiveTabs() map[string]int {
	ws := m.activeWS()
	if ws == nil || ws.Panes == nil {
		return nil
	}
	out := map[string]int{}
	for _, key := range ws.Panes.Keys() {
		switch inst := ws.Panes.Get(key); {
		case inst == nil:
		case inst.Kind() == pane.KindEditor:
			out[key] = inst.ActiveTab()
		case inst.Kind() == pane.KindTerminal:
			out[key] = 0
		}
	}
	return out
}

// restoreActiveToolTabs re-selects each pane's per-project active tab after
// the switch-in global tool attach (#1906): first every pane goes back to the
// index it held before the attach appended its tabs, then the global tools
// this project itself had active reclaim their tab wherever it landed. A
// remembered tool that did not come back (closed everywhere, or no longer
// configured global here) contributes nothing, so the pre-attach index stands
// and no pane is ever left pointing at a tab that is gone.
func (m *Model) restoreActiveToolTabs(pre map[string]int) {
	ws := m.activeWS()
	if ws == nil || ws.Panes == nil {
		return
	}
	changed := false
	for key, idx := range pre {
		inst := ws.Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor || idx >= inst.TabCount() {
			continue
		}
		if idx != inst.ActiveTab() && inst.ActivateTab(idx) {
			changed = true
		}
	}
	for _, name := range ws.ActiveTools {
		locs := m.toolLocations(name)
		if len(locs) != 1 || locs[0].tab < 0 {
			continue // gone, or dedicated: nothing to select
		}
		inst := ws.Panes.Get(locs[0].key)
		if inst == nil || locs[0].tab == inst.ActiveTab() {
			continue
		}
		if inst.ActivateTab(locs[0].tab) {
			changed = true
		}
	}
	if changed {
		// attachGlobalToolIn saved the layout with the arriving tool tab
		// active; persist the corrected selection over it.
		saveLayout(ws.Tree, ws.Panes)
	}
}

// staleGlobalTool reports a global tool a saved layout lists that must not
// restore (#1903): no session is parked and the tool was explicitly closed
// in some project since the layout was saved. The manager — stash plus
// closed set — is the authority on whether a global tool is open, never a
// per-project layout.json.
func (m *Model) staleGlobalTool(entry config.ToolEntry) bool {
	if !entry.Global || m.ws == nil {
		return false
	}
	if _, parked := m.ws.PeekGlobalTool(entry.Name); parked {
		return false
	}
	return m.ws.GlobalToolClosed(entry.Name)
}

// noteGlobalToolCloses records the explicit close of every global tool
// session inst still hosts (#1903): a close in one project ends the tool
// everywhere, so stale layout entries elsewhere must not resurrect it on the
// next switch. Move/drag paths detach the session before closing the vacated
// pane, so a relocation never counts as a close.
func (m *Model) noteGlobalToolCloses(inst *pane.Instance) {
	if inst == nil {
		return
	}
	switch inst.Kind() {
	case pane.KindTerminal:
		m.noteGlobalToolTabClose(inst.Terminal())
	case pane.KindEditor:
		for i := 0; i < inst.TabCount(); i++ {
			m.noteGlobalToolTabClose(inst.TabTerminal(i))
		}
	}
}

// noteGlobalToolTabClose is noteGlobalToolCloses for one hosted terminal.
func (m *Model) noteGlobalToolTabClose(t *terminal.Model) {
	if t == nil || m.ws == nil {
		return
	}
	if entry, ok := globalToolEntry(t.Tool()); ok {
		m.ws.MarkGlobalToolClosed(entry.Name)
	}
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

// parkedGlobalToolExited reports whether sess is a parked global tool's
// session. The dead session deliberately stays in the stash — exit status
// and grid intact (#1903) — so the next switch-in (attachOpenGlobalTools) or
// tool.<name> materializes the pane with the standard #810 exited overlay,
// where Restart reruns the command in place as the same global session. (The
// pre-#1903 reap closed the stashed session silently and the tool vanished
// without a dialog.)
func (m *Model) parkedGlobalToolExited(sess string) bool {
	for _, name := range m.ws.GlobalToolNames() {
		if term, ok := m.ws.PeekGlobalTool(name); ok && term.SessionKey() == sess {
			return true
		}
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
		// Spawning fresh reopens the tool: forget a recorded close (#1903).
		m.ws.ClearGlobalToolClosed(entry.Name)
	}
	dir := entry.Cwd
	if dir == "" {
		dir = "."
	}
	argv := append([]string{entry.Command}, entry.Args...)
	return reg.NewToolSession(entry.Name, argv, dir, toolSpawnEnv(m.pal()), m.host.Send)
}

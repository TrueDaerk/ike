package app

import (
	"ike/internal/layout"
	"ike/internal/pane"
)

// layouts_slots.go reconciles saved window layouts (#1175) with named slot
// templates (#1897): the current slot config is authoritative for slotted
// tools (#1899). Applying a snapshot while a template is active prunes every
// snapshot leaf the template claims — dedicated tool leaves, pure tool tab
// hosts, singleton panels with a slot assignment — before the ordinary apply
// and re-opens them through the slot engine afterwards, so they land at the
// template's exact position and proportions, tools sharing a slot restore as
// tabs, and the snapshot's raw position is only a hint for non-slotted
// panes. Live slotted panes survive the apply in their slots (running
// terminals are never killed, and a slotted tool grafted into the flexible
// region would fight the template on the next open); singleton panels absent
// from a full layout keep the #791 hide semantics. A tool that was slotted
// at save time but is not any more applies as an ordinary snapshot leaf —
// both directions of a config/snapshot mismatch resolve to "current slot
// config wins". Without an active template nothing here runs and apply is
// exactly #1175/#1568/#1577.

// slotOpen is one slotted occupant a pruned snapshot leaf asks for: a custom
// tool by name, or a singleton panel by identity kind.
type slotOpen struct {
	slot string
	kind string // identity kind: "tool" or a singleton panel kind
	tool string // tool name when kind == "tool"
}

// slotResident is one live pane the active template claims: the pane key,
// the slot its tool/key is assigned to, and whether it is a singleton panel
// (panels hide on a full-layout apply, #791, instead of surviving in-slot).
type slotResident struct {
	key, slot string
	panel     bool
}

// singletonSlotKey maps a snapshot singleton identity kind to its fixed pane
// key — the join point between kind-only snapshot identities and the
// key-based [tools.layout] assign entries.
func singletonSlotKey(kind string) string {
	switch kind {
	case "explorer":
		return pane.ExplorerKey
	case "vcs":
		return pane.VCSKey
	case "debug":
		return pane.DebugKey
	case "problems":
		return pane.ProblemsKey
	case "tests":
		return pane.TestsKey
	case "issues":
		return pane.IssuesKey
	case "structure":
		return pane.StructureKey
	case "usages":
		return pane.UsagesKey
	case "http":
		return pane.HTTPKey
	case "breakpoints":
		return pane.BreakpointsKey
	}
	return ""
}

// snapshotSlotOpens classifies one snapshot identity under the active
// template: the slot opens it stands for when the leaf must re-materialize
// through the slot engine, ok=false when the leaf applies as saved. An
// editor identity hosting tool tabs is claimed only when every tool is
// slot-assigned (a pure tool host); a mixed host — some tools unassigned, or
// file tabs alongside — stays main content at its snapshot position.
func snapshotSlotOpens(tpl *layout.Template, id paneIdentity) ([]slotOpen, bool) {
	assigned := func(tool string) (string, bool) {
		slot := toolSlot(tool)
		return slot, slot != "" && tpl.HasSlot(slot)
	}
	switch id.Kind {
	case "tool":
		if slot, ok := assigned(id.Tool); ok {
			return []slotOpen{{slot: slot, kind: "tool", tool: id.Tool}}, true
		}
	case "editor":
		if len(id.Tools) == 0 {
			return nil, false
		}
		var opens []slotOpen
		for _, tool := range id.Tools {
			slot, ok := assigned(tool)
			if !ok {
				return nil, false
			}
			opens = append(opens, slotOpen{slot: slot, kind: "tool", tool: tool})
		}
		return opens, true
	default:
		if key := singletonSlotKey(id.Kind); key != "" {
			if slot, ok := assigned(key); ok {
				return []slotOpen{{slot: slot, kind: id.Kind}}, true
			}
		}
	}
	return nil, false
}

// pruneSlottedLeaves detaches every snapshot leaf the active template claims
// (the #1568 pruning generalized), returning the pruned tree plus the slot
// opens in leaf walk order. When the removals would empty the tree the last
// slotted leaf degrades to a scratch editor slot instead, so the apply keeps
// a position to re-slot the editor region into.
func pruneSlottedLeaves(tree layout.Node, ids map[string]paneIdentity, tpl *layout.Template) (layout.Node, []slotOpen) {
	var opens []slotOpen
	for _, key := range layout.Leaves(tree) {
		o, slotted := snapshotSlotOpens(tpl, ids[key])
		if !slotted {
			continue
		}
		opens = append(opens, o...)
		if pruned, ok := layout.Close(tree, key); ok {
			tree = pruned
			continue
		}
		ids[key] = paneIdentity{Kind: "editor"}
	}
	return tree, opens
}

// liveSlotted lists the live panes the active template claims — dedicated
// tool panes, pure tool tab hosts (#836) and singleton panels whose tool or
// key is slot-assigned — in registry order. Unlike slotResidents it reports
// every claimant, wherever its leaf currently sits: a slotted tool the user
// moved out of its slot still re-materializes in the slot on apply.
func (m *Model) liveSlotted(tpl *layout.Template) []slotResident {
	ws := m.activeWS()
	if ws.Tree == nil {
		return nil
	}
	var out []slotResident
	inTree := layout.Panes(ws.Tree)
	for _, key := range ws.Panes.Keys() {
		if !inTree[key] {
			continue
		}
		inst := ws.Panes.Get(key)
		if inst == nil {
			continue
		}
		slot, panel := "", false
		switch {
		case inst.Kind() == pane.KindTerminal && inst.Terminal().Tool() != "":
			slot = toolSlot(inst.Terminal().Tool())
		case inst.Kind() == pane.KindEditor:
			slot = tabHostSlot(inst)
		case isToolKind(inst.Kind()):
			slot = toolSlot(key)
			panel = true
		}
		if slot != "" && tpl.HasSlot(slot) {
			out = append(out, slotResident{key: key, slot: slot, panel: panel})
		}
	}
	return out
}

// applySlots re-materializes the slotted occupants after a snapshot apply:
// live slotted panes first (their leaves were held out of the apply and the
// graft), then the snapshot's slot opens that no live pane already stands in
// for. Everything goes through placePaneInSlot / the tab-in-slot rule, so
// positions, proportions and tab grouping are exactly what a runtime open
// would produce (#1897) and the derived slot bookkeeping is consistent by
// construction. A placement failure (a pathological tree of nothing but
// slotted panes) leaves the pane registered but leafless — its toggle
// resurfaces it, the toolhide precedent (#791).
func (m *Model) applySlots(tpl *layout.Template, live []slotResident, opens []slotOpen, st *applyState) {
	reg := m.activeWS().Panes
	placed := map[string]int{} // live tool instances standing in for snapshot opens
	for _, r := range live {
		inst := reg.Get(r.key)
		if inst == nil || !m.placePaneInSlot(tpl, r.slot, r.key) {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			placed[inst.Terminal().Tool()]++
		case pane.KindEditor:
			for i := 0; i < inst.TabCount(); i++ {
				if t := inst.TabTerminal(i); t != nil && t.Tool() != "" {
					placed[t.Tool()]++
				}
			}
		}
	}
	for _, o := range opens {
		if o.kind != "tool" {
			m.openPanelAtSlot(tpl, o, st)
			continue
		}
		if placed[o.tool] > 0 {
			placed[o.tool]-- // a live instance stands in for the snapshot's
			continue
		}
		m.restoreToolAtSlot(tpl, o)
	}
}

// openPanelAtSlot resurfaces a singleton panel pruned from the snapshot into
// its assigned slot, creating and wiring the instance when needed — the same
// wiring resolveLeaf gives a panel restored at its snapshot position.
func (m *Model) openPanelAtSlot(tpl *layout.Template, o slotOpen, st *applyState) {
	key := singletonSlotKey(o.kind)
	if key == "" || layout.Panes(m.activeWS().Tree)[key] {
		return // already standing (a live claimant re-placed), or malformed
	}
	resolved, ok := m.resolveLeaf(paneIdentity{Kind: o.kind}, st)
	if !ok {
		return // the same singleton twice: the first open won
	}
	m.placePaneInSlot(tpl, o.slot, resolved)
}

// restoreToolAtSlot restarts a snapshot's slotted tool through the slot open
// path (#1897): a resident that can host tabs takes it as a tab — tools
// sharing a slot restore as tabs, global tools included (#1901) — anything
// else gets a dedicated pane at the template position. A parked live global
// instance re-attaches instead of spawning a duplicate (#1890); a tool no
// longer configured restores as nothing, its slot simply staying collapsed.
func (m *Model) restoreToolAtSlot(tpl *layout.Template, o slotOpen) {
	entry, ok := toolEntry(o.tool)
	if !ok {
		return
	}
	reg := m.activeWS().Panes
	if resident := m.slotResidents()[o.slot]; resident != "" &&
		canHostTabs(reg.Get(resident)) && m.ensureTabHost(resident) {
		reg.Get(resident).AddTerminalTab(m.restoredToolSession(reg, entry))
		return
	}
	key := reg.AddTerminalPaneFrom(m.restoredToolSession(reg, entry))
	if !m.placePaneInSlot(tpl, o.slot, key) {
		if entry.Global {
			m.reparkGlobalToolPane(entry.Name, key)
			return
		}
		reg.Close(key)
	}
}

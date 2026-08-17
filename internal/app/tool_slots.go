package app

import (
	"strings"

	"ike/internal/config"
	"ike/internal/layout"
	"ike/internal/pane"
)

// tool_slots.go implements named-slot tool placement (#1897): the
// [tools.layout] template declares exact positions for tool windows around
// the editor region, and every assigned tool opens by rebuilding the outer
// slot structure deterministically — no dock heuristics. The editor region
// keeps its own splits untouched: materializing a slot peels the currently
// slotted panes off the tree, grafts the remainder into the template's E
// position and re-places every resident at its template ratio. Closing a
// slotted pane needs no code here at all: the ordinary leaf-close collapse
// hands its space to the surviving sibling, which is exactly the template's
// absorb-on-collapse rule. Tools without an assignment keep the #1889 home
// position / adaptive behavior.

// slotTemplate returns the parsed [tools.layout] template, nil when slot
// placement is off. Validation already cleared structurally broken
// templates, so a parse failure here only happens mid-reload; parsing on
// every open keeps the template in lockstep with config reloads without a
// cache to invalidate (the grids are a handful of cells).
func slotTemplate() *layout.Template {
	c := config.Get()
	if c == nil || len(c.Tools.Layout.Template) == 0 {
		return nil
	}
	tpl, err := layout.ParseTemplate(c.Tools.Layout.Template)
	if err != nil {
		return nil
	}
	return tpl
}

// toolSlot resolves the slot assigned to a tool id — a [[tools.custom]]
// name, a built-in tool-window pane key (explorer, vcs, debug, problems,
// structure, usages, http, breakpoints) or the Run tool (run, #1905) — ""
// when unassigned. Assignments were normalized to "SLOT=tool" by validation.
func toolSlot(tool string) string {
	c := config.Get()
	if c == nil {
		return ""
	}
	for _, a := range c.Tools.Layout.Assign {
		if slot, name, ok := strings.Cut(a, "="); ok && name == tool {
			return slot
		}
	}
	return ""
}

// assignedSlot resolves the template slot a tool actually opens at: its
// assignment, but only while a template defining that slot is active. It
// returns the template alongside the slot name, "" when slot placement does
// not apply to the tool.
func assignedSlot(tool string) (*layout.Template, string) {
	tpl := slotTemplate()
	if tpl == nil {
		return nil, ""
	}
	slot := toolSlot(tool)
	if slot == "" || !tpl.HasSlot(slot) {
		return nil, ""
	}
	return tpl, slot
}

// slotResidents maps each slot to the pane currently occupying it in the
// active workspace's tree: a dedicated tool pane whose tool is assigned to
// the slot, a singleton tool window keyed by its assigned id, or a tab host
// carrying only tool sessions (#836). Residency is derived, never stored —
// that is what keeps layout.json round-trips and config edits trivially
// consistent. When two panes claim one slot (a subdivided slot, see
// placePaneInSlot), the first in registry order wins.
func (m *Model) slotResidents() map[string]string {
	out := map[string]string{}
	ws := m.activeWS()
	if ws.Tree == nil {
		return out
	}
	inTree := layout.Panes(ws.Tree)
	for _, key := range ws.Panes.Keys() {
		if !inTree[key] {
			continue
		}
		inst := ws.Panes.Get(key)
		if inst == nil {
			continue
		}
		slot := ""
		switch {
		case inst.Kind() == pane.KindTerminal && inst.Terminal().Tool() != "":
			slot = toolSlot(inst.Terminal().Tool())
		case inst.Kind() == pane.KindEditor:
			slot = tabHostSlot(inst)
		case isToolKind(inst.Kind()):
			slot = toolSlot(key)
		}
		if slot != "" {
			if _, taken := out[slot]; !taken {
				out[slot] = key
			}
		}
	}
	return out
}

// tabHostSlot resolves the slot an editor-kind pane occupies as a converted
// tool tab host: every tab must be a terminal session — an editor showing
// documents is main content even when a tool tab was dragged into it — and
// the slot is the first hosted tool's assignment.
func tabHostSlot(inst *pane.Instance) string {
	n := inst.TabCount()
	if n == 0 {
		return ""
	}
	slot := ""
	for i := 0; i < n; i++ {
		t := inst.TabTerminal(i)
		if t == nil {
			return ""
		}
		if slot == "" && t.Tool() != "" {
			slot = toolSlot(t.Tool())
		}
	}
	return slot
}

// materializeSlot rebuilds the workspace tree with newKey occupying slot:
// the current residents peel off (RemoveLeaves), the remaining editor
// region is grafted into the template's E position, and every resident —
// newKey included — lands at its slot's exact template position and
// proportions. The rebuild works on a clone and commits only on success, so
// a pathological tree (nothing but slotted panes) leaves the layout
// untouched.
func (m *Model) materializeSlot(tpl *layout.Template, slot, newKey string) bool {
	ws := m.activeWS()
	if ws.Tree == nil {
		return false
	}
	residents := m.slotResidents()
	drop := map[string]bool{}
	for _, key := range residents {
		drop[key] = true
	}
	editor, ok := layout.RemoveLeaves(layout.Clone(ws.Tree), drop)
	if !ok {
		return false
	}
	residents[slot] = newKey
	ws.Tree = tpl.BuildTree(residents, editor)
	return true
}

// slotShareZone picks how a second, non-tabbable occupant shares a slot:
// slots wider than tall (in template cells) split side by side, others
// stack — deterministic from the template alone, unlike the workspace-size
// auxZone heuristic.
func slotShareZone(tpl *layout.Template, slot string) layout.Zone {
	if r, ok := tpl.SlotRect(slot); ok && r.W > r.H {
		return layout.ZoneRight
	}
	return layout.ZoneBottom
}

// placePaneInSlot inserts the already-registered pane key into slot: a free
// slot materializes at its template position, an occupied one subdivides
// deterministically along its longer template axis (tab-joining an occupied
// slot is the caller's business — singleton panels never tab). The tree is
// unchanged on failure.
func (m *Model) placePaneInSlot(tpl *layout.Template, slot, key string) bool {
	ws := m.activeWS()
	if resident := m.slotResidents()[slot]; resident != "" {
		tree, ok := layout.SplitLeaf(ws.Tree, resident, key, slotShareZone(tpl, slot))
		if !ok {
			return false
		}
		ws.Tree = tree
		return true
	}
	return m.materializeSlot(tpl, slot, key)
}

// insertToolPane places a freshly created tool-window pane: an assigned
// slot (#1897) pins it to the template position; otherwise — or when the
// slot cannot be materialized — the pane splits off target at zone, the
// shared tail of every built-in panel opener. Reports ok; the caller drops
// the pane instance on failure.
func (m *Model) insertToolPane(key, target string, zone layout.Zone) bool {
	if tpl := slotTemplate(); tpl != nil {
		if slot := toolSlot(key); slot != "" && tpl.HasSlot(slot) {
			if m.placePaneInSlot(tpl, slot, key) {
				return true
			}
		}
	}
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, zone)
	if !ok {
		return false
	}
	m.activeWS().Tree = tree
	return true
}

// openToolAtSlot spawns a tool pinned to its assigned slot (#1897): a free
// slot materializes at the template position; a slot held by a tab-capable
// pane adds the tool as a focused tab — several tools sharing one slot become
// tabs (#836), global tools included (#1901: the workspace switch extracts a
// hosted global session tab-wise) — and a non-tabbable holder (a singleton
// panel) subdivides the slot along its longer template axis instead.
func (m *Model) openToolAtSlot(sp toolSpawn, tpl *layout.Template, slot string) bool {
	ws := m.activeWS()
	if resident := m.slotResidents()[slot]; resident != "" &&
		canHostTabs(ws.Panes.Get(resident)) && m.ensureTabHost(resident) {
		ws.Panes.Get(resident).AddTerminalTab(m.newToolTab(sp))
		m.setFocus(resident)
		m.rememberTool(sp.name, resident)
		m.layout()
		saveLayout(ws.Tree, ws.Panes)
		return true
	}
	key := m.addToolPane(sp)
	if !m.placePaneInSlot(tpl, slot, key) {
		ws.Panes.Close(key)
		return false
	}
	m.setFocus(key)
	m.rememberTool(sp.name, key)
	m.layout()
	saveLayout(ws.Tree, ws.Panes)
	return true
}

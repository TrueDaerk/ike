package app

import (
	"ike/internal/pane"
)

// layouts_toolwindows.go — tool windows in named layouts (#2736). A tab host may
// carry singleton tool windows as content tabs; a snapshot records them as
// kind-only CTabs of the host's slot, and an apply puts every saved window
// back where the layout says — a dedicated slot or a host's tab strip —
// adopting the live instance across shapes before ever building a fresh one
// (the #2124 rule for tool sessions, applied to windows).

// hostedWindowTabs lists the tool windows a tab host carries as content tabs,
// as kind-only content identities in tab order.
func hostedWindowTabs(inst *pane.Instance) []contentTabIdentity {
	var out []contentTabIdentity
	for i := 0; i < inst.TabCount(); i++ {
		c := inst.TabContent(i)
		if c == nil {
			continue
		}
		if name := pane.ToolWindowName(c.Kind()); name != "" {
			out = append(out, contentTabIdentity{Kind: name, Index: i, Pinned: inst.TabPinned(i)})
		}
	}
	return out
}

// hostToolIDs names everything a tab host hosts that a "tools" slot matches
// by: tool session names and hosted tool-window kinds.
func hostToolIDs(inst *pane.Instance) []string {
	tools, _ := editorPaneTools(inst)
	for _, ct := range hostedWindowTabs(inst) {
		tools = append(tools, ct.Kind)
	}
	return tools
}

// savedToolIDs is hostToolIDs for a saved slot identity.
func savedToolIDs(id paneIdentity) []string {
	out := append([]string(nil), id.Tools...)
	for _, ct := range id.CTabs {
		if _, ok := pane.ToolWindowKind(ct.Kind); ok {
			out = append(out, ct.Kind)
		}
	}
	return out
}

// dedicatedToolWindow returns the key of the tool window of kind as a
// dedicated pane for a singleton slot: the registered pane when there is one,
// else the live hosted tab detached into its own pane, else a fresh window.
func (m *Model) dedicatedToolWindow(st *applyState, kind pane.Kind) string {
	reg := m.activeWS().Panes
	if key := pane.SingletonKey(kind); reg.Has(key) {
		return key
	}
	if nested, ok := m.detachHostedWindow(st, kind); ok {
		if key, ok := reg.AddContentPaneFrom(nested); ok {
			return key
		}
	}
	return reg.AddToolWindow(kind)
}

// detachHostedWindow takes the live tool window of kind out of the tab host
// it lives in. A host drained of its sole tab closes when it is still queued
// for a slot (a scratch tab covers DetachContentTab's last-tab refusal, the
// #1901 pattern); a host already resolved as a slot keeps the scratch tab so
// its leaf stays backed.
func (m *Model) detachHostedWindow(st *applyState, kind pane.Kind) (*pane.Instance, bool) {
	reg := m.activeWS().Panes
	hostKey, tabIdx, _, ok := m.toolWindowAt(kind)
	if !ok || tabIdx < 0 {
		return nil, false
	}
	host := reg.Get(hostKey)
	sole := host.TabCount() == 1
	if sole {
		host.AddTab()
	}
	nested, ok := host.DetachContentTab(tabIdx)
	if !ok {
		return nil, false
	}
	if sole && dequeueHost(st, hostKey) {
		reg.Close(hostKey)
	}
	return nested, true
}

// dequeueHost removes key from the apply queues, reporting whether it was
// still waiting for a slot.
func dequeueHost(st *applyState, key string) bool {
	found := false
	for _, queue := range []*[]string{&st.hosts, &st.content} {
		for i, k := range *queue {
			if k == key {
				*queue = append((*queue)[:i], (*queue)[i+1:]...)
				found = true
				break
			}
		}
	}
	return found
}

// restoreWindowTabs puts the saved tool windows of a host slot into the
// pane at key as content tabs: a window already there stays, a live one
// elsewhere — hosted tab or dedicated pane the layout has no slot for —
// moves in with its state, a closed one is built fresh and wired. A window
// a resolved dedicated slot already owns is left there (a snapshot naming it
// twice). fresh marks a pane minted for the slot, whose placeholder scratch
// tab gives way once a window is in.
func (m *Model) restoreWindowTabs(reg *pane.Registry, st *applyState, key string, saved []contentTabIdentity, fresh bool) {
	host := reg.Get(key)
	if host == nil || host.Kind() != pane.KindEditor {
		return
	}
	added := 0
	for _, ct := range saved {
		kind, ok := pane.ToolWindowKind(ct.Kind)
		if !ok {
			continue
		}
		hostKey, tabIdx, inst, live := m.toolWindowAt(kind)
		var nested *pane.Instance
		switch {
		case live && hostKey == key:
			continue // already a tab of this host
		case live && tabIdx >= 0:
			nested, _ = m.detachHostedWindow(st, kind)
		case live && st.used[hostKey]:
			continue // a dedicated slot of this layout owns it
		case live:
			// A dedicated pane the layout does not slot moves in whole; the
			// husk closes and its old leaf is pruned by the graft.
			nested, _ = inst.DetachContent()
			reg.Close(hostKey)
		default:
			nested = reg.NewContentPane(kind, "", "", "", "")
			m.wireToolWindow(nested)
		}
		if nested == nil || !host.AddContentTab(nested) {
			continue
		}
		if ct.Pinned {
			host.SetTabPinned(host.ActiveTab(), true)
		}
		added++
	}
	if fresh && added > 0 && host.TabCount() > added {
		if ed := host.TabEditor(0); ed != nil && !ed.HasFile() {
			host.CloseTab(0) // drop the placeholder scratch tab
		}
	}
}

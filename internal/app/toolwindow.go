package app

import (
	"ike/internal/deps"
	"ike/internal/forge"
	"ike/internal/pane"
)

// toolwindow.go — the singleton tool windows wherever they live (#2736). A
// window (Problems, HTTP, VCS, Debug, …) is one instance per workspace, but
// since #2736 that instance may be a dedicated pane under its fixed key or a
// content tab nested in a tab host, whose nested instance carries the same
// key. Every lookup that used to be `Panes.Has(pane.XKey)` resolves through
// here, so the toggle commands, the result routers and the session hooks
// find the window in either shape and never spawn a second one.

// toolWindowIn finds the tool window of kind in reg: the host key, the tab
// index (-1 for a dedicated pane) and the instance; ok=false when the window
// is closed.
func toolWindowIn(reg *pane.Registry, kind pane.Kind) (hostKey string, tabIdx int, inst *pane.Instance, ok bool) {
	forEachContent(reg, func(key string, idx int, c *pane.Instance) bool {
		if c.Kind() == kind {
			hostKey, tabIdx, inst, ok = key, idx, c, true
			return false
		}
		return true
	})
	return hostKey, tabIdx, inst, ok
}

// toolWindowRestored reports whether reg already holds the tool window of
// kind in either shape — the restore's duplicate guard.
func toolWindowRestored(reg *pane.Registry, kind pane.Kind) bool {
	_, _, _, ok := toolWindowIn(reg, kind)
	return ok
}

// hostedToolWindows collects the tool-window kinds a persisted layout hosts
// as content tabs of its tab hosts (#2736), so the restore can tell a window
// that belongs in a host from one that owns a leaf.
func hostedToolWindows(ids map[string]paneIdentity) map[pane.Kind]bool {
	out := map[pane.Kind]bool{}
	for _, id := range ids {
		for _, ct := range id.CTabs {
			if kind, ok := pane.ToolWindowKind(ct.Kind); ok {
				out[kind] = true
			}
		}
	}
	return out
}

// toolWindowAt is toolWindowIn over the active workspace.
func (m Model) toolWindowAt(kind pane.Kind) (hostKey string, tabIdx int, inst *pane.Instance, ok bool) {
	return toolWindowIn(m.activeWS().Panes, kind)
}

// toolWindow returns the live instance of the tool window of kind, nil while
// it is closed — the nest-aware form of `Panes.Get(pane.XKey)`.
func (m Model) toolWindow(kind pane.Kind) *pane.Instance {
	_, _, inst, ok := m.toolWindowAt(kind)
	if !ok {
		return nil
	}
	return inst
}

// toolWindowOpen reports whether the tool window of kind exists anywhere in
// the active workspace — the nest-aware form of `Panes.Has(pane.XKey)`.
func (m Model) toolWindowOpen(kind pane.Kind) bool {
	return m.toolWindow(kind) != nil
}

// toolWindowFocused reports whether the keyboard reaches the tool window of
// kind: its dedicated pane is focused, or its host is focused with the
// window's tab active.
func (m Model) toolWindowFocused(kind pane.Kind) bool {
	hostKey, tabIdx, _, ok := m.toolWindowAt(kind)
	if !ok || m.activeWS().Panes.Focused() != hostKey {
		return false
	}
	if tabIdx < 0 {
		return true
	}
	host := m.activeWS().Panes.Get(hostKey)
	return host != nil && host.ActiveTab() == tabIdx
}

// focusToolWindow focuses the tool window of kind wherever it lives,
// activating its tab in a host; it reports whether the window exists.
func (m *Model) focusToolWindow(kind pane.Kind) bool {
	hostKey, tabIdx, _, ok := m.toolWindowAt(kind)
	if !ok {
		return false
	}
	m.focusContentAt(hostKey, tabIdx)
	return true
}

// toolWindowLeaf returns the layout leaf key showing the tool window of kind
// and whether the window is on screen: a dedicated pane under a visible
// leaf, or a host whose active tab is the window. A background tab or a
// leafless (hidden) window reports false — the popup anchors and visibility
// checks that used `m.lay.Panes[pane.XKey]` need exactly that.
func (m Model) toolWindowLeaf(kind pane.Kind) (string, bool) {
	hostKey, tabIdx, _, ok := m.toolWindowAt(kind)
	if !ok || !m.leafVisible(hostKey) {
		return "", false
	}
	if tabIdx >= 0 {
		host := m.activeWS().Panes.Get(hostKey)
		if host == nil || host.ActiveTab() != tabIdx {
			return "", false
		}
	}
	return hostKey, true
}

// closeToolWindow closes the tool window of kind wherever it lives: a
// dedicated pane closes as a pane, a hosted tab closes as a tab, and a host
// whose only tab is the window closes whole. It reports whether anything
// closed.
func (m *Model) closeToolWindow(kind pane.Kind) bool {
	hostKey, tabIdx, _, ok := m.toolWindowAt(kind)
	if !ok {
		return false
	}
	host := m.activeWS().Panes.Get(hostKey)
	if tabIdx < 0 || host == nil || host.TabCount() <= 1 {
		m.closePane(hostKey)
		return true
	}
	m.closeTab(host, tabIdx)
	m.layout()
	return true
}

// wireToolWindow gives a freshly built tool window the app-side hooks its
// open path injects (#2736): the shared stores, display-path formatter,
// forge factories and loading flags every restore used to spell out per
// kind. It runs for dedicated panes and nested tabs alike — the session
// restore, the named-layout apply and the tab restore all pass through here
// — and is a no-op for kinds that need no wiring.
func (m *Model) wireToolWindow(inst *pane.Instance) {
	if inst == nil {
		return
	}
	switch inst.Kind() {
	case pane.KindProblems:
		// Diagnostics are session state (#1024); the live store re-feeds the
		// panel as the language servers publish.
		p := inst.Problems()
		p.SetDisplayPath(displayPath)
		p.SetStore(m.probStore)
	case pane.KindTime:
		// The aggregate is re-read from the usage log in the background (#2426).
		inst.Time().SetLoading(true)
	case pane.KindUsage:
		// Same for the Usage panel (#2552).
		inst.Usage().SetLoading(true)
	case pane.KindDeps:
		// The panel re-seeds from the last snapshot; the auto-scan (or 'r')
		// refreshes it (#2419).
		p := inst.Deps()
		p.SetDisplayPath(displayPath)
		p.Set(deps.Snapshot())
	case pane.KindUsages:
		// Find-references results are session state (#1155); the next
		// lsp.referencesPanel run re-fills it.
		inst.Usages().SetDisplayPath(displayPath)
	case pane.KindIssues:
		// The same factories openIssuesPanel injects — refresh, timeline
		// (#2084), mutations (#2088) and the metadata probe the edit gating
		// reads (#2087). Without them a restored pane would come back
		// read-only; 'r' re-fetches the listing and runs the probe.
		p := inst.Issues()
		p.SetRefresh(forge.RefreshFactory("."))
		p.SetTimeline(forge.TimelineFactory("."))
		p.SetMutate(forge.MutateFactory("."))
		p.SetMeta(forge.MetaFactory("."))
		p.SetPRDetailFetch(forge.PRDetailFactory("."))
		p.SetPRAction(forge.PRActionFactory("."))
	case pane.KindBreakpoints:
		// Seeded from the persisted store loaded at start (#1377).
		m.wireBreakpointsPanel(inst.Breakpoints())
	case pane.KindDoctor:
		// Shares the app-owned trace log (#1991).
		m.wireDoctorPanel(inst.Doctor())
	case pane.KindLSPDoctor:
		// Shares the app-owned report (#2164).
		m.wireLSPDoctorPanel(inst.LSPDoctor())
	}
}

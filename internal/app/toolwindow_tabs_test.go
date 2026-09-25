package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// toolwindow_tabs_test.go covers #2736: every pane but the explorer merges as
// a tab — a tool pane onto the HTTP viewer, the HTTP viewer onto a tool
// window and back, the toggle commands finding a hosted window, a hosted
// window splitting back out intact, and both persistence paths (session
// layout.json, named layouts) round-tripping tool windows hosted as tabs.

// windowCount counts live instances of kind across panes and hosted tabs.
func windowCount(m Model, kind pane.Kind) int {
	n := 0
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		if c.Kind() == kind {
			n++
		}
		return true
	})
	return n
}

// dragPaneOnto title-drags the whole pane src and releases it in the center
// of pane dst.
func dragPaneOnto(t *testing.T, m Model, src, dst string) Model {
	t.Helper()
	sr, ok := m.lay.Panes[src]
	if !ok {
		t.Fatalf("setup: no laid-out pane %q", src)
	}
	m = step(m, press(sr.X+2, sr.Y+1))
	dr, ok := m.lay.Panes[dst]
	if !ok {
		t.Fatalf("setup: no laid-out pane %q", dst)
	}
	return step(m, release(dr.X+dr.W/2, dr.Y+dr.H/2))
}

// httpAndProblems opens the HTTP viewer and the Problems window as dedicated
// panes and returns the model.
func httpAndProblems(t *testing.T) Model {
	t.Helper()
	m := dismissOnboarding(sized(t, 140, 50)) // the first-start dialog would eat the mouse
	m.openHTTPPanel()
	m.toggleProblemsPanel()
	m.layout()
	if !m.activeWS().Panes.Has(pane.HTTPKey) || !m.activeWS().Panes.Has(pane.ProblemsKey) {
		t.Fatal("setup: both windows must open as dedicated panes")
	}
	return m
}

// TestToolPaneDropOnHTTPCenterMergesAsTab (#2736): a terminal-backed tool
// pane dragged onto the HTTP viewer's center merges as a tab — the viewer
// converts into a tab host, keeping its live model as the first tab under
// its own key, and the host takes a fresh key.
func TestToolPaneDropOnHTTPCenterMergesAsTab(t *testing.T) {
	withTools(t, sleepTool("watcher"))
	m := dismissOnboarding(sized(t, 140, 50))
	t.Cleanup(func() { closeAllSessions(m) })
	out, _ := m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	toolKey := m.activeWS().Panes.Focused()
	m.openHTTPPanel()
	m.layout()
	before := m.httpPanel()
	if before == nil {
		t.Fatal("setup: HTTP viewer must be open and visible")
	}

	m = dragPaneOnto(t, m, toolKey, pane.HTTPKey)

	if m.activeWS().Panes.Has(toolKey) {
		t.Fatal("the vacated tool pane must close")
	}
	if m.activeWS().Panes.Has(pane.HTTPKey) {
		t.Fatal("the singleton key must belong to the hosted window, not to the host")
	}
	hostKey, tabIdx, nested, ok := m.toolWindowAt(pane.KindHTTP)
	if !ok || tabIdx != 0 || nested.Key() != pane.HTTPKey {
		t.Fatalf("the viewer must live as the host's first tab under its key: host=%q tab=%d ok=%v", hostKey, tabIdx, ok)
	}
	host := m.activeWS().Panes.Get(hostKey)
	if host == nil || host.Kind() != pane.KindEditor || host.TabCount() != 2 {
		t.Fatalf("host = %v, want an editor-kind pane with 2 tabs", host)
	}
	if tt := host.TabTerminal(1); tt == nil || tt.Tool() != "watcher" || !tt.Running() {
		t.Fatal("the tool session must join as a running terminal tab")
	}
	if m.activeWS().Panes.Focused() != hostKey || !leafSet(m)[hostKey] {
		t.Fatal("focus and the layout leaf must follow the host's new key")
	}
	if m.httpPanel() == nil {
		t.Fatal("the HTTP viewer must still resolve through its nest-aware lookup")
	}
	if got := hostToolIDs(host); len(got) != 2 {
		t.Fatalf("hostToolIDs = %v, want the session and the window", got)
	}
}

// TestHTTPPaneDropOnToolWindowCenterMergesAsTab (#2736): the HTTP viewer
// dragged onto a tool window's center merges as a tab of that window, and
// the reverse direction works the same way.
func TestHTTPPaneDropOnToolWindowCenterMergesAsTab(t *testing.T) {
	for _, tc := range []struct{ name, src, dst string }{
		{"http onto problems", pane.HTTPKey, pane.ProblemsKey},
		{"problems onto http", pane.ProblemsKey, pane.HTTPKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := httpAndProblems(t)
			m = dragPaneOnto(t, m, tc.src, tc.dst)
			if m.activeWS().Panes.Has(tc.src) || m.activeWS().Panes.Has(tc.dst) {
				t.Fatal("both singleton keys must be free: the host carries a fresh key")
			}
			hHost, hTab, _, hOK := m.toolWindowAt(pane.KindHTTP)
			pHost, pTab, _, pOK := m.toolWindowAt(pane.KindProblems)
			if !hOK || !pOK || hHost != pHost || hTab < 0 || pTab < 0 || hTab == pTab {
				t.Fatalf("both windows must be tabs of one host: http=%q/%d problems=%q/%d", hHost, hTab, pHost, pTab)
			}
			host := m.activeWS().Panes.Get(hHost)
			if host.TabCount() != 2 || host.TabContent(0).Key() != tc.dst {
				t.Fatalf("the target's window must stay the first tab, tabs=%d first=%q", host.TabCount(), host.TabContent(0).Key())
			}
			if windowCount(m, pane.KindHTTP) != 1 || windowCount(m, pane.KindProblems) != 1 {
				t.Fatal("each window must exist exactly once")
			}
			if m.problemsPanel() == nil || m.httpPanel() == nil {
				t.Fatal("both accessors must resolve the hosted windows")
			}
			if m.activeWS().Panes.Focused() != hHost {
				t.Fatal("focus must land on the host")
			}
		})
	}
}

// TestExplorerNeverMergesAsTab (#2736): the explorer is the single
// exception — it offers no center zone to a tool-window drag and never
// converts, in either direction.
func TestExplorerNeverMergesAsTab(t *testing.T) {
	m := httpAndProblems(t)
	er := m.lay.Panes[pane.ExplorerKey]
	d := &dragState{kind: dragMove, srcPane: pane.HTTPKey, curX: er.X + er.W/2, curY: er.Y + er.H/2}
	if zone, can := m.dropZoneFor(d, pane.ExplorerKey, er); can && zone == layout.ZoneCenter {
		t.Fatal("the explorer must not offer the center merge zone")
	}
	if !m.dragCarriesContent(d) {
		t.Fatal("a tool-window drag carries content a host could adopt")
	}
	hr := m.lay.Panes[pane.HTTPKey]
	d = &dragState{kind: dragMove, srcPane: pane.ExplorerKey, curX: hr.X + hr.W/2, curY: hr.Y + hr.H/2}
	if zone, _ := m.dropZoneFor(d, pane.HTTPKey, hr); zone == layout.ZoneCenter {
		t.Fatal("an explorer drag carries nothing to merge: no center zone on the viewer")
	}
	if _, ok := m.ensureTabHost(pane.ExplorerKey); ok {
		t.Fatal("the explorer must never convert into a tab host")
	}
	if m.activeWS().Panes.Get(pane.ExplorerKey).Kind() != pane.KindExplorer {
		t.Fatal("the explorer must stay an explorer")
	}
}

// TestToggleFocusesHostedToolWindow (#2736): problems.toggle on a window
// hosted as a tab focuses that tab instead of opening a second instance, and
// a second toggle hands focus back.
func TestToggleFocusesHostedToolWindow(t *testing.T) {
	m := httpAndProblems(t)
	m = dragPaneOnto(t, m, pane.ProblemsKey, pane.HTTPKey)
	hostKey, pTab, _, _ := m.toolWindowAt(pane.KindProblems)
	host := m.activeWS().Panes.Get(hostKey)
	host.ActivateTab(0) // the HTTP tab
	edKey := m.activeEditorKey()
	m.setFocus(edKey)

	m.toggleProblemsPanel()
	if m.activeWS().Panes.Focused() != hostKey || host.ActiveTab() != pTab {
		t.Fatalf("toggle must focus the hosted tab: focus=%q active=%d want %q/%d", m.activeWS().Panes.Focused(), host.ActiveTab(), hostKey, pTab)
	}
	if windowCount(m, pane.KindProblems) != 1 || m.activeWS().Panes.Has(pane.ProblemsKey) {
		t.Fatal("the toggle must not spawn a second Problems window")
	}
	m.toggleProblemsPanel()
	if m.activeWS().Panes.Focused() != edKey {
		t.Fatalf("the second toggle must return focus to %q, got %q", edKey, m.activeWS().Panes.Focused())
	}
	host.ActivateTab(0)
	m.toggleProblemsPanel()
	if host.ActiveTab() != pTab {
		t.Fatal("toggling while the host shows another tab must switch to the window")
	}
	// ensure/show follow the same rule.
	m.setFocus(edKey)
	if cmd := m.ensurePanel(pane.ProblemsKey, func() tea.Cmd { t.Fatal("ensurePanel must not reopen a hosted window"); return nil }); cmd != nil {
		t.Fatal("ensurePanel returned a command for an open window")
	}
	m.showPanel(pane.ProblemsKey, func() tea.Cmd { t.Fatal("showPanel must not reopen a hosted window"); return nil })
	if m.activeWS().Panes.Focused() != hostKey || host.ActiveTab() != pTab {
		t.Fatal("showPanel must focus the hosted tab")
	}
}

// TestHostedToolWindowSplitsBackOutIntact (#2736): dragging a hosted tool
// window's tab onto the host's edge restores a dedicated pane of the
// original kind under its fixed key, the live model moving with it.
func TestHostedToolWindowSplitsBackOutIntact(t *testing.T) {
	m := httpAndProblems(t)
	m = dragPaneOnto(t, m, pane.ProblemsKey, pane.HTTPKey)
	hostKey, pTab, nested, _ := m.toolWindowAt(pane.KindProblems)
	model := nested.Problems()
	r := m.lay.Panes[hostKey]
	m.drag = &dragState{kind: dragTab, srcPane: hostKey, srcTab: pTab, startX: r.X + 2, startY: r.Y + 1, curX: r.X + 1, curY: r.Y + r.H/2}
	m.commitTabMove(r.X+1, r.Y+r.H/2)
	m.drag = nil

	if !m.activeWS().Panes.Has(pane.ProblemsKey) {
		t.Fatal("the window must become a dedicated pane under its fixed key again")
	}
	inst := m.activeWS().Panes.Get(pane.ProblemsKey)
	if inst.Kind() != pane.KindProblems || inst.Problems() != model {
		t.Fatal("the dedicated pane must carry the same live model")
	}
	if !leafSet(m)[pane.ProblemsKey] {
		t.Fatal("the dedicated pane must be a layout leaf")
	}
	if host := m.activeWS().Panes.Get(hostKey); host == nil || host.TabCount() != 1 {
		t.Fatal("the host keeps the HTTP tab alone")
	}
	if windowCount(m, pane.KindProblems) != 1 {
		t.Fatal("exactly one Problems window must exist")
	}
}

// TestHostedToolWindowsPersistAcrossRestore (#2736): a tab host holding the
// HTTP viewer and the Problems window saves and restores with both as tabs
// — no dedicated leaf, no duplicate, the app hooks wired.
func TestHostedToolWindowsPersistAcrossRestore(t *testing.T) {
	m := httpAndProblems(t)
	m = dragPaneOnto(t, m, pane.ProblemsKey, pane.HTTPKey)
	hostKey, _, _, _ := m.toolWindowAt(pane.KindHTTP)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)

	m2 := NewWith(registry.New(), host.MapConfig{})
	out, _ := m2.Update(tea.WindowSizeMsg{Width: 140, Height: 50})
	m2 = out.(Model)
	restored := m2.activeWS().Panes.Get(hostKey)
	if restored == nil || restored.Kind() != pane.KindEditor || restored.TabCount() != 2 {
		t.Fatalf("host did not restore under %q with 2 tabs", hostKey)
	}
	if c := restored.TabContent(0); c == nil || c.Kind() != pane.KindHTTP || c.Key() != pane.HTTPKey {
		t.Fatal("the HTTP viewer must restore as the first tab under its key")
	}
	if c := restored.TabContent(1); c == nil || c.Kind() != pane.KindProblems || c.Key() != pane.ProblemsKey {
		t.Fatal("the Problems window must restore as the second tab under its key")
	}
	if m2.activeWS().Panes.Has(pane.HTTPKey) || m2.activeWS().Panes.Has(pane.ProblemsKey) || leafSet(m2)[pane.ProblemsKey] {
		t.Fatal("no dedicated window may restore beside the hosted tabs")
	}
	if windowCount(m2, pane.KindProblems) != 1 || windowCount(m2, pane.KindHTTP) != 1 {
		t.Fatal("each window restores exactly once")
	}
	if m2.problemsPanel() == nil || m2.httpPanel() == nil {
		t.Fatal("the restored windows must resolve through the accessors")
	}
}

// TestLayoutNamingWindowTwiceRestoresOnce (#2736): a layout.json naming a
// window both as a dedicated leaf and as a hosted tab restores it once, in
// the host — the dedicated leaf prunes.
func TestLayoutNamingWindowTwiceRestoresOnce(t *testing.T) {
	m := httpAndProblems(t)
	m = dragPaneOnto(t, m, pane.ProblemsKey, pane.HTTPKey)
	hostKey, _, _, _ := m.toolWindowAt(pane.KindHTTP)
	// Forge a duplicate: a dedicated "problems" leaf beside the host.
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, hostKey, pane.ProblemsKey, layout.ZoneBottom)
	if !ok {
		t.Fatal("setup: split failed")
	}
	m.activeWS().Tree = tree
	m.activeWS().Panes.AddToolWindow(pane.KindProblems) // a second registry entry for the save
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)

	m2 := NewWith(registry.New(), host.MapConfig{})
	if windowCount(m2, pane.KindProblems) != 1 {
		t.Fatalf("Problems windows = %d, want 1", windowCount(m2, pane.KindProblems))
	}
	if _, tabIdx, _, ok := m2.toolWindowAt(pane.KindProblems); !ok || tabIdx < 0 {
		t.Fatal("the hosted copy wins; the dedicated leaf prunes")
	}
	if leafSet(m2)[pane.ProblemsKey] {
		t.Fatal("the dedicated leaf must not restore")
	}
}

// TestNamedLayoutRoundTripsHostedToolWindows (#2736): a named layout saved
// with tool windows hosted as tabs snapshots them on the host's slot and,
// on apply, puts them back as tabs — adopting the live windows across
// shapes, never duplicating, and building them fresh in a workspace where
// they are closed.
func TestNamedLayoutRoundTripsHostedToolWindows(t *testing.T) {
	m := httpAndProblems(t)
	m = dragPaneOnto(t, m, pane.ProblemsKey, pane.HTTPKey)
	hostKey, _, _, _ := m.toolWindowAt(pane.KindHTTP)
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	var hostID paneIdentity
	found := false
	for _, id := range snap.Panes {
		if len(id.CTabs) == 2 {
			hostID, found = id, true
		}
	}
	if !found || hostID.Kind != "tools" || hostID.CTabs[0].Kind != "http" || hostID.CTabs[1].Kind != "problems" {
		t.Fatalf("the host must snapshot as a tools slot carrying both windows, got %+v", snap.Panes)
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})

	// Apply onto the live arrangement: the hosted windows are adopted.
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	if windowCount(m, pane.KindHTTP) != 1 || windowCount(m, pane.KindProblems) != 1 {
		t.Fatal("apply must not duplicate the live hosted windows")
	}
	if h, tabIdx, _, ok := m.toolWindowAt(pane.KindProblems); !ok || tabIdx < 0 || h != hostKey {
		t.Fatal("the live host re-slots with its windows")
	}

	// Apply after the windows moved back out to dedicated panes: they are
	// adopted into the slot's host, the husks close.
	m2 := httpAndProblems(t)
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}}) // sized re-pointed IKE_CONFIG_DIR
	m2 = step(m2, ApplyLayoutMsg{Name: "dev"})
	if windowCount(m2, pane.KindHTTP) != 1 || windowCount(m2, pane.KindProblems) != 1 {
		t.Fatal("apply must move dedicated windows into the host, not duplicate them")
	}
	h2, hTab, _, _ := m2.toolWindowAt(pane.KindHTTP)
	p2, pTab, _, _ := m2.toolWindowAt(pane.KindProblems)
	if hTab < 0 || pTab < 0 || h2 != p2 {
		t.Fatalf("both windows must be tabs of one host after apply: http=%q/%d problems=%q/%d", h2, hTab, p2, pTab)
	}
	if m2.activeWS().Panes.Has(pane.HTTPKey) || leafSet(m2)[pane.ProblemsKey] {
		t.Fatal("the dedicated panes must be gone")
	}

	// Apply in a workspace where the windows are closed: built fresh, wired.
	m3 := dismissOnboarding(sized(t, 140, 50))
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})
	m3 = step(m3, ApplyLayoutMsg{Name: "dev"})
	if windowCount(m3, pane.KindHTTP) != 1 || windowCount(m3, pane.KindProblems) != 1 {
		t.Fatal("apply must build the saved windows once each")
	}
	if _, tabIdx, _, ok := m3.toolWindowAt(pane.KindProblems); !ok || tabIdx < 0 {
		t.Fatal("a fresh window restores as a tab of the slot's host")
	}
	if m3.problemsPanel() == nil {
		t.Fatal("the fresh window must resolve through the accessor")
	}
}

// TestNamedLayoutDedicatedSlotAdoptsHostedWindow (#2736): the reverse — a
// layout with dedicated HTTP and Problems slots applied while both windows
// are hosted as tabs detaches them into their slots, once each.
func TestNamedLayoutDedicatedSlotAdoptsHostedWindow(t *testing.T) {
	split := httpAndProblems(t)
	snap, ok := snapshotLayout(split.activeWS().Tree, split.activeWS().Panes)
	if !ok || snap.Panes[pane.HTTPKey].Kind != "http" || snap.Panes[pane.ProblemsKey].Kind != "problems" {
		t.Fatalf("setup: dedicated windows must snapshot as singleton slots, got %+v", snap.Panes)
	}
	m := httpAndProblems(t)
	m = dragPaneOnto(t, m, pane.ProblemsKey, pane.HTTPKey)
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"split": snap}}) // after sized re-pointed IKE_CONFIG_DIR
	m = step(m, ApplyLayoutMsg{Name: "split"})
	for _, kind := range []pane.Kind{pane.KindHTTP, pane.KindProblems} {
		if windowCount(m, kind) != 1 {
			t.Fatalf("kind %d: windows = %d, want 1", kind, windowCount(m, kind))
		}
		key := pane.SingletonKey(kind)
		if _, tabIdx, _, ok := m.toolWindowAt(kind); !ok || tabIdx >= 0 || !m.activeWS().Panes.Has(key) || !leafSet(m)[key] {
			t.Fatalf("kind %d: the window must re-slot as its dedicated leaf %q", kind, key)
		}
	}
}

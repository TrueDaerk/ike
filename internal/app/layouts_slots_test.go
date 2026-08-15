package app

import (
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// layouts_slots_test.go covers the interaction of saved window layouts
// (#1175) with named slot templates (#1897), defined in #1899: slotted tools
// in a snapshot re-materialize through the slot engine at the template's
// exact geometry, save→apply round-trips tabs-per-slot without duplicates,
// live slotted panes survive an apply in their slots, and the current slot
// config wins over a snapshot saved under a different config.

// closeLeafTerminals ends every terminal session still holding a leaf —
// dedicated panes and tab-hosted sessions — so sleepTool processes never
// outlive a test.
func closeLeafTerminals(m Model) {
	for key := range leafSet(m) {
		inst := m.activeWS().Panes.Get(key)
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
}

// assertIssueTemplateGeometry checks the applied tree against the normative
// XEEH/XEEH/TTZZ template with T and Z occupied by the given panes and the
// explorer at X: the bottom strip at the 1/3 cell fraction, split in halves.
func assertIssueTemplateGeometry(t *testing.T, m Model, tKey, zKey string) {
	t.Helper()
	root, ok := m.activeWS().Tree.(*layout.Split)
	if !ok || root.Orient != layout.Vertical {
		t.Fatalf("root = %#v, want the template's vertical strip split", m.activeWS().Tree)
	}
	ratioNear(t, root.Ratio, 2.0/3.0, "strip")
	strip, ok := root.B.(*layout.Split)
	if !ok || strip.Orient != layout.Horizontal {
		t.Fatalf("strip = %#v, want the TT|ZZ split", root.B)
	}
	ratioNear(t, strip.Ratio, 0.5, "TT|ZZ")
	if a, ok := strip.A.(*layout.Leaf); !ok || a.Pane != tKey {
		t.Fatalf("strip left = %#v, want T %q", strip.A, tKey)
	}
	if b, ok := strip.B.(*layout.Leaf); !ok || b.Pane != zKey {
		t.Fatalf("strip right = %#v, want Z %q", strip.B, zKey)
	}
	top, ok := root.A.(*layout.Split)
	if !ok || top.Orient != layout.Horizontal {
		t.Fatalf("top region = %#v, want explorer|editor", root.A)
	}
	ratioNear(t, top.Ratio, 0.25, "explorer column")
	if l, ok := top.A.(*layout.Leaf); !ok || l.Pane != pane.ExplorerKey {
		t.Fatalf("X slot = %#v, want the explorer", top.A)
	}
}

// TestApplyLayoutSlotRoundTrip: save while two slotted tools and the slotted
// explorer are open, resize, apply — the same live panes come back at the
// template's exact geometry, nothing duplicates, and the derived slot
// bookkeeping matches the tree.
func TestApplyLayoutSlotRoundTrip(t *testing.T) {
	issueLayout(t,
		[]string{"X=explorer", "T=toolt", "Z=toolz"},
		sleepTool("toolt"), sleepTool("toolz"))
	m := sized(t, 100, 40)
	m = step(m, ToolOpenMsg{Name: "toolt"})
	m = step(m, ToolOpenMsg{Name: "toolz"})
	tKey := m.toolPane("toolt").Key()
	zKey := m.toolPane("toolz").Key()
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})
	before := leafSet(m)

	// Distort the strip ratio; apply must snap it back to the template's.
	m.activeWS().Tree.(*layout.Split).Ratio = 0.9
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	defer closeLeafTerminals(m)

	assertIssueTemplateGeometry(t, m, tKey, zKey)
	after := leafSet(m)
	if len(after) != len(before) {
		t.Fatalf("leaf count changed: before %v, after %v", before, after)
	}
	for key := range before {
		if !after[key] {
			t.Fatalf("pane %q lost across the round trip (after: %v)", key, after)
		}
	}
	res := m.slotResidents()
	if res["T"] != tKey || res["Z"] != zKey || res["X"] != pane.ExplorerKey {
		t.Fatalf("slot bookkeeping inconsistent after apply: %v", res)
	}
}

// TestApplyLayoutSlotTabsRoundTrip: two tools sharing one slot as tabs
// (#836) round-trip as one slot pane with both tabs — live panes are reused
// on an immediate re-apply, and a cold apply restarts both tools tabbed in
// the slot instead of as separate leaves.
func TestApplyLayoutSlotTabsRoundTrip(t *testing.T) {
	issueLayout(t,
		[]string{"T=ta", "T=tb"},
		sleepTool("ta"), sleepTool("tb"))
	m := sized(t, 100, 40)
	m = step(m, ToolOpenMsg{Name: "ta"})
	hostKey := m.toolPane("ta").Key()
	m = step(m, ToolOpenMsg{Name: "tb"})
	host := m.activeWS().Panes.Get(hostKey)
	if host == nil || host.Kind() != pane.KindEditor || host.TabCount() != 2 {
		t.Fatalf("precondition: both tools must share the T slot as tabs, got %#v", host)
	}
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})

	// Live re-apply: the host pane survives with both tabs, no duplicates.
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	if !leafSet(m)[hostKey] {
		t.Fatalf("live slot host must keep its leaf, leaves %v", leafSet(m))
	}
	if got := m.activeWS().Panes.Get(hostKey).TabCount(); got != 2 {
		t.Fatalf("host tab count after live apply = %d, want 2", got)
	}
	tools, _ := leafToolCounts(m)
	if len(tools) != 0 {
		t.Fatalf("no dedicated tool panes expected next to the tab host, got %v", tools)
	}

	// Cold apply: close everything, apply — both tools restart tabbed in T.
	host = m.activeWS().Panes.Get(hostKey)
	host.CloseTerminalTabs()
	m.activeWS().Panes.Close(hostKey)
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	defer closeLeafTerminals(m)
	resident := m.slotResidents()["T"]
	if resident == "" {
		t.Fatalf("T slot empty after cold apply, tree %v", leafSet(m))
	}
	inst := m.activeWS().Panes.Get(resident)
	if inst.TabCount() != 2 {
		t.Fatalf("cold apply must restart both tools as tabs in T, got %d tabs", inst.TabCount())
	}
}

// TestApplyLayoutGlobalSlotTab: a global tool sharing a slot as a tab (#1901)
// round-trips a named layout: a live re-apply keeps the single host with both
// tabs, and an apply while the global session is parked (the switch seam)
// re-attaches it as the missing tab instead of spawning a duplicate.
func TestApplyLayoutGlobalSlotTab(t *testing.T) {
	global := sleepTool("tg")
	global.Global = true
	issueLayout(t,
		[]string{"T=ta", "T=tg"},
		sleepTool("ta"), global)
	m := sized(t, 100, 40)
	m = step(m, ToolOpenMsg{Name: "ta"})
	hostKey := m.toolPane("ta").Key()
	m = step(m, ToolOpenMsg{Name: "tg"})
	host := m.activeWS().Panes.Get(hostKey)
	if host == nil || host.TabCount() != 2 {
		t.Fatalf("precondition: the global tool must tab into the T slot, got %#v", host)
	}
	sess := host.TabTerminal(host.ActiveTab()).SessionKey()
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})

	// Live re-apply: one host, two tabs, no duplicate global instance.
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	if got := m.activeWS().Panes.Get(hostKey).TabCount(); got != 2 {
		t.Fatalf("host tab count after live apply = %d, want 2", got)
	}
	if locs := m.toolLocations("tg"); len(locs) != 1 {
		t.Fatalf("global tool locations after live apply = %+v, want exactly one", locs)
	}

	// Park the global session (the switch seam), then apply: the parked live
	// session re-attaches as the missing tab with the same session key.
	m.detachGlobalTools()
	if _, ok := m.ws.PeekGlobalTool("tg"); !ok {
		t.Fatal("detach must park the tab-hosted global session")
	}
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	defer closeLeafTerminals(m)
	locs := m.toolLocations("tg")
	if len(locs) != 1 || locs[0].tab < 0 {
		t.Fatalf("global tool must re-attach as a slot tab, locations %+v", locs)
	}
	if got := m.activeWS().Panes.Get(locs[0].key).TabTerminal(locs[0].tab).SessionKey(); got != sess {
		t.Fatalf("re-attached session = %q, want the parked one %q", got, sess)
	}
	if _, ok := m.ws.PeekGlobalTool("tg"); ok {
		t.Fatal("the re-attached session must leave the manager stash")
	}
}

// TestApplyLayoutSlottedNowButNotAtSave pins the mismatch rule: a snapshot
// saved without any template restores its tool through the slot engine once
// the tool is assigned — the snapshot position is only a hint for
// non-slotted panes, the current slot config wins.
func TestApplyLayoutSlottedNowButNotAtSave(t *testing.T) {
	withTools(t, sleepTool("toolt"))
	m := sized(t, 100, 40)
	m = step(m, ToolOpenMsg{Name: "toolt"}) // adaptive placement, no template
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})
	closeToolInstances(m, "toolt")
	if inst := m.toolPane("toolt"); inst != nil {
		m.activeWS().Panes.Close(inst.Key())
	}

	// Now the template assigns the tool; apply must use the slot, not the
	// snapshot's adaptive position.
	issueLayout(t, []string{"T=toolt"}, sleepTool("toolt"))
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	defer closeLeafTerminals(m)
	tKey := m.slotResidents()["T"]
	if tKey == "" {
		t.Fatalf("tool must restart in its slot, residents %v", m.slotResidents())
	}
	root, ok := m.activeWS().Tree.(*layout.Split)
	if !ok || root.Orient != layout.Vertical {
		t.Fatalf("root = %#v, want the template's vertical strip split", m.activeWS().Tree)
	}
	ratioNear(t, root.Ratio, 2.0/3.0, "strip")
	if l, ok := root.B.(*layout.Leaf); !ok || l.Pane != tKey {
		t.Fatalf("bottom strip = %#v, want the T pane %q", root.B, tKey)
	}
}

// TestApplyLayoutKeepsOpenSlottedTool: applying a layout that does not
// contain a currently open slotted tool keeps the tool in its slot — running
// terminals are never killed (#1175) and slots stay authoritative — and a
// subsequent slotted open still follows #1897 (Z subdivides the strip).
func TestApplyLayoutKeepsOpenSlottedTool(t *testing.T) {
	issueLayout(t,
		[]string{"X=explorer", "T=toolt", "Z=toolz"},
		sleepTool("toolt"), sleepTool("toolz"))
	m := sized(t, 100, 40)
	m = step(m, ToolOpenMsg{Name: "toolt"})
	tKey := m.toolPane("toolt").Key()
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"plain": twoPaneSnapshot(t)}})

	m = step(m, ApplyLayoutMsg{Name: "plain"})
	defer closeLeafTerminals(m)
	if m.slotResidents()["T"] != tKey {
		t.Fatalf("open slotted tool must survive the apply in its slot, residents %v", m.slotResidents())
	}
	root, ok := m.activeWS().Tree.(*layout.Split)
	if !ok || root.Orient != layout.Vertical {
		t.Fatalf("root = %#v, want the template's vertical strip split", m.activeWS().Tree)
	}
	ratioNear(t, root.Ratio, 2.0/3.0, "strip")
	if l, ok := root.B.(*layout.Leaf); !ok || l.Pane != tKey {
		t.Fatalf("bottom strip = %#v, want T %q full-width", root.B, tKey)
	}
	// The snapshot's slotted explorer landed in X, not at its raw position.
	if m.slotResidents()["X"] != pane.ExplorerKey {
		t.Fatalf("snapshot explorer must land in X, residents %v", m.slotResidents())
	}

	// Post-apply opens keep #1897 semantics: Z subdivides the strip.
	m = step(m, ToolOpenMsg{Name: "toolz"})
	zKey := m.toolPane("toolz").Key()
	assertIssueTemplateGeometry(t, m, tKey, zKey)
}

// TestRestoreDefaultLayoutHonorsSlots: window.restoreLayout with a saved
// default containing a slotted tool re-materializes the tool in its slot.
func TestRestoreDefaultLayoutHonorsSlots(t *testing.T) {
	issueLayout(t, []string{"T=toolt"}, sleepTool("toolt"))
	m := sized(t, 100, 40)
	m = step(m, ToolOpenMsg{Name: "toolt"})
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}, Default: "dev"})
	closeToolInstances(m, "toolt")
	if inst := m.toolPane("toolt"); inst != nil {
		m.activeWS().Panes.Close(inst.Key())
	}

	m = step(m, RestoreDefaultLayoutMsg{})
	defer closeLeafTerminals(m)
	tKey := m.slotResidents()["T"]
	if tKey == "" {
		t.Fatalf("default layout must restart the tool in its slot, residents %v", m.slotResidents())
	}
	root, ok := m.activeWS().Tree.(*layout.Split)
	if !ok || root.Orient != layout.Vertical {
		t.Fatalf("root = %#v, want the template's vertical strip split", m.activeWS().Tree)
	}
	if l, ok := root.B.(*layout.Leaf); !ok || l.Pane != tKey {
		t.Fatalf("bottom strip = %#v, want the restarted T pane %q", root.B, tKey)
	}
}

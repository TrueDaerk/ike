package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// run_placement_test.go guards where run output lands (#2191): the Run tool
// joins the layout's existing tool region instead of always re-rooting the
// tree at the outermost bottom, reruns reuse it, and a Run pane the user
// dragged somewhere else keeps that position for later runs — across a
// restart, per project.

// runPaneKey returns the key of the Run tool's dedicated pane, failing when
// none is open.
func runPaneKey(t *testing.T, m Model) string {
	t.Helper()
	locs := m.toolLocations(runToolName)
	if len(locs) != 1 {
		t.Fatalf("Run tool instances = %d, want exactly 1", len(locs))
	}
	if locs[0].tab >= 0 {
		t.Fatalf("Run tool is a tab of %q, want a dedicated pane", locs[0].key)
	}
	return locs[0].key
}

// neighbour describes where a leaf hangs in the tree: the pane it sits beside
// and the side that neighbour occupies.
func neighbour(t *testing.T, m Model, key string) (string, layout.Zone) {
	t.Helper()
	hops := layout.Hops(m.activeWS().Tree, key)
	if len(hops) == 0 {
		t.Fatalf("pane %q has no parent split", key)
	}
	return layout.EdgeLeafIn(hops[0].Sibling, layout.Opposite(hops[0].Zone)), hops[0].Zone
}

// TestRunOpensInNestedToolRegion is the #2191 regression: with the tool strip
// inside the editor column — the shape every bottom-split panel produces once
// the explorer is beside it — a run must join that strip instead of splitting
// the whole workspace at its outermost bottom.
func TestRunOpensInNestedToolRegion(t *testing.T) {
	m := runModel(t, "bottom")
	tm, _ := m.Update(ProblemsToggleMsg{})
	m = tm.(Model)
	if !m.activeWS().Panes.Has(pane.ProblemsKey) {
		t.Fatal("setup: the Problems panel must open")
	}
	root, ok := m.activeWS().Tree.(*layout.Split)
	if !ok || root.Orient != layout.Horizontal {
		t.Fatalf("setup: root = %#v, want the explorer column beside the editor column", m.activeWS().Tree)
	}
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneBottom); got != "" {
		t.Fatalf("setup: workspace bottom edge = %q, want no dock slot (the bug's precondition)", got)
	}

	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)
	key := runPaneKey(t, m)

	root, ok = m.activeWS().Tree.(*layout.Split)
	if !ok || root.Orient != layout.Horizontal {
		t.Fatalf("root = %#v, want the tree un-rerooted", m.activeWS().Tree)
	}
	if l, ok := root.A.(*layout.Leaf); !ok || l.Pane != pane.ExplorerKey {
		t.Fatalf("root.A = %#v, want the explorer column left untouched", root.A)
	}
	mate, zone := neighbour(t, m, key)
	if mate != pane.ProblemsKey {
		t.Fatalf("Run pane neighbour = %q, want the tool region's %q", mate, pane.ProblemsKey)
	}
	if zone != layout.ZoneLeft {
		t.Fatalf("Problems sits %v of the Run pane, want ZoneLeft (they share the strip)", zone)
	}

	// Re-running reuses that pane instead of stacking another split.
	panes := m.activeWS().Panes.Len()
	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)
	if got := m.activeWS().Panes.Len(); got != panes {
		t.Fatalf("panes after rerun = %d, want %d (the Run tool is reused)", got, panes)
	}
	if got := runPaneKey(t, m); got != key {
		t.Fatalf("rerun landed in %q, want the existing Run pane %q", got, key)
	}
}

// TestRunKeepsWorkspaceDockWithoutToolRegion: an editor below the editor is
// no tool region, so the plain #1889 full-span bottom dock still applies.
func TestRunKeepsWorkspaceDockWithoutToolRegion(t *testing.T) {
	m := runModel(t, "bottom")
	path := m.activeFilePath()
	tm, _ := m.Update(SplitFocusedMsg{Zone: layout.ZoneBottom})
	m = tm.(Model)
	// The fresh split is empty; give it the same file so the run has a target.
	tm, _ = m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)
	key := runPaneKey(t, m)
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneBottom); got != key {
		t.Fatalf("bottom edge leaf = %q, want the Run pane %q docked full-span", got, key)
	}
}

// dragPane drags a pane's title bar onto a point, the way a user relocates it.
// The grab is the title *text* row: the border row above it doubles as the
// divider band, where a press starts a resize instead (layout.Hit).
func dragPane(t *testing.T, m Model, key string, x, y int) Model {
	t.Helper()
	r, ok := m.lay.Panes[key]
	if !ok {
		t.Fatalf("pane %q has no rectangle", key)
	}
	m = step(m, press(r.X+2, r.Y+1))
	m = step(m, motion(x, y))
	return step(m, release(x, y))
}

// TestRunHomeFollowsUserMove covers the relocation rule: once the Run pane is
// dragged elsewhere, closing it and running again brings it back *there*, not
// at the configured placement.
func TestRunHomeFollowsUserMove(t *testing.T) {
	m := runModel(t, "bottom")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	key := runPaneKey(t, m)
	edKey := m.activeEditorKey()
	if edKey == "" {
		t.Fatal("setup: an editor pane must exist")
	}

	ed := m.lay.Panes[edKey]
	m = dragPane(t, m, key, ed.X+ed.W-2, ed.Y+ed.H/2)
	if mate, zone := neighbour(t, m, key); mate != edKey || zone != layout.ZoneLeft {
		t.Fatalf("after the move the Run pane sits %v of %q, want right of the editor", zone, mate)
	}
	h, ok := loadRunHome()
	if !ok {
		t.Fatal("the move must record the Run tool's new home")
	}
	if h.Anchor != edKey || h.Zone != "right" || h.Tab {
		t.Fatalf("recorded home = %+v, want the editor's right side", h)
	}

	// Close it — the next run must not fall back to the bottom dock.
	m.closePane(key)
	if len(m.toolLocations(runToolName)) != 0 {
		t.Fatal("setup: the Run pane must be closed")
	}
	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)
	reopened := runPaneKey(t, m)
	if mate, zone := neighbour(t, m, reopened); mate != edKey || zone != layout.ZoneLeft {
		t.Fatalf("reopened Run pane sits %v of %q, want right of the editor again", zone, mate)
	}
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneBottom); got == reopened {
		t.Fatal("the reopened Run pane went back to the workspace bottom dock")
	}
}

// TestRunHomeFollowsOuterDock: dragging the Run pane onto a workspace edge
// strip (#811) is remembered as a full-span dock, not as a split off whichever
// leaf happened to touch it.
func TestRunHomeFollowsOuterDock(t *testing.T) {
	m := runModel(t, "bottom")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	key := runPaneKey(t, m)

	body := m.bodyRect()
	m = dragPane(t, m, key, body.X+body.W-1, body.Y+body.H/2) // outer right edge
	h, ok := loadRunHome()
	if !ok || !h.Root || h.Zone != "right" {
		t.Fatalf("recorded home = %+v (ok=%v), want a full-span right dock", h, ok)
	}

	m.closePane(key)
	tm, _ = m.Update(RunFileMsg{})
	m = tm.(Model)
	reopened := runPaneKey(t, m)
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneRight); got != reopened {
		t.Fatalf("right edge leaf = %q, want the reopened Run pane %q", got, reopened)
	}
	if r := m.lay.Panes[reopened]; r.H != body.H {
		t.Fatalf("reopened Run pane height = %d, want the full body height %d", r.H, body.H)
	}
}

// TestRunHomeSurvivesRestart: the moved position is per-project state, so a
// fresh session — which prunes the Run leaf on restore (#1905) — still runs
// into the pane the user chose.
func TestRunHomeSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	path := filepath.Join(t.TempDir(), "prog.rfake")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := host.MapConfig{"run.placement": "bottom"}
	m := NewWith(registry.New(), cfg)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(explorer.OpenFileMsg{Path: path})
	tm, _ = tm.(Model).Update(RunFileMsg{})
	m = tm.(Model)
	key := runPaneKey(t, m)
	edKey := m.activeEditorKey()
	ed := m.lay.Panes[edKey]
	m = dragPane(t, m, key, ed.X+ed.W-2, ed.Y+ed.H/2)
	if _, ok := loadRunHome(); !ok {
		t.Fatal("setup: the move must record a home")
	}

	// A fresh model over the same project state: layout.json restores the
	// panes, the Run leaf prunes, and the rerun re-opens at the moved spot.
	m2 := NewWith(registry.New(), cfg)
	out, _ := m2.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 = out.(Model)
	if len(m2.toolLocations(runToolName)) != 0 {
		t.Fatal("the restore must prune the Run tool leaf (#1905)")
	}
	out, _ = m2.Update(RunRerunMsg{})
	m2 = out.(Model)
	reopened := runPaneKey(t, m2)
	if mate, zone := neighbour(t, m2, reopened); mate != edKey || zone != layout.ZoneLeft {
		t.Fatalf("after the restart the Run pane sits %v of %q, want right of %q", zone, mate, edKey)
	}
}

// TestRunHomeIgnoredAfterPlacementChange: the record names the placement it
// overruled, so changing run.placement re-asserts the setting instead of
// being shadowed by an old drag.
func TestRunHomeIgnoredAfterPlacementChange(t *testing.T) {
	m := runModel(t, "bottom")
	saveRunHome(runHome{Anchor: pane.ExplorerKey, Zone: "bottom", Placement: "left"})
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	key := runPaneKey(t, m)
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneBottom); got != key {
		t.Fatalf("bottom edge leaf = %q, want the Run pane %q at the configured placement", got, key)
	}
}

// TestRunHomeIgnoredWhenAnchorGone: a record pointing at a pane this
// workspace no longer has falls back to the placement rules.
func TestRunHomeIgnoredWhenAnchorGone(t *testing.T) {
	m := runModel(t, "bottom")
	saveRunHome(runHome{Anchor: "editor:9", Zone: "right", Placement: "bottom"})
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	key := runPaneKey(t, m)
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneBottom); got != key {
		t.Fatalf("bottom edge leaf = %q, want the Run pane %q at the configured placement", got, key)
	}
}

package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// universaltabs_test.go covers #1778: tabs as a universal pane facility —
// viewer panes drag into tab hosts, viewer panes convert into tab hosts,
// content tabs drag back out into dedicated panes, mixed strips persist, and
// singleton tool windows stay edge-only drop targets.

// mdPreviewModel builds a sized model showing a markdown file plus its
// preview pane, returning the model, the editor key and the preview key.
func mdPreviewModel(t *testing.T) (Model, string, string) {
	t.Helper()
	m := sized(t, 120, 40)
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte("# Drag\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	edKey := m.activeWS().Panes.Focused()
	tm, _ = m.Update(MarkdownPreviewMsg{})
	m = tm.(Model)
	pvKey := previewKeyFor(m, path)
	if pvKey == "" {
		t.Fatal("setup: no preview pane")
	}
	return m, edKey, pvKey
}

// TestViewerPaneDropOnEditorCenterBecomesTab (#1778): dragging a whole
// markdown preview pane onto an editor pane's center merges its live content
// into the tab strip and closes the vacated pane.
func TestViewerPaneDropOnEditorCenterBecomesTab(t *testing.T) {
	m, edKey, pvKey := mdPreviewModel(t)
	pr := m.lay.Panes[pvKey]
	m = step(m, press(pr.X+2, pr.Y+1))
	dst := m.lay.Panes[edKey]
	m = step(m, release(dst.X+dst.W/2, dst.Y+dst.H/2))

	if m.activeWS().Panes.Has(pvKey) {
		t.Fatal("the vacated preview pane must close")
	}
	inst := m.activeWS().Panes.Get(edKey)
	if inst == nil || inst.TabCount() != 2 {
		t.Fatalf("editor tabs = %d, want 2 (file + preview)", inst.TabCount())
	}
	c := inst.TabContent(inst.ActiveTab())
	if c == nil || c.Kind() != pane.KindMarkdown {
		t.Fatal("the preview must arrive as the active content tab")
	}
	if m.activeWS().Panes.Focused() != edKey {
		t.Fatal("focus must land on the merge target")
	}
}

// TestEditorPaneDropOnViewerCenterConvertsHost (#1778): dragging an editor
// pane with an open file onto a viewer pane's center converts the viewer
// into a tab host — its content stays as the first tab, the file joins it.
func TestEditorPaneDropOnViewerCenterConvertsHost(t *testing.T) {
	m, edKey, pvKey := mdPreviewModel(t)
	er := m.lay.Panes[edKey]
	m = step(m, press(er.X+2, er.Y+1))
	dst := m.lay.Panes[pvKey]
	m = step(m, release(dst.X+dst.W/2, dst.Y+dst.H/2))

	if m.activeWS().Panes.Has(edKey) {
		t.Fatal("the emptied editor pane must close")
	}
	inst := m.activeWS().Panes.Get(pvKey)
	if inst == nil || inst.Kind() != pane.KindEditor {
		t.Fatal("the viewer pane must convert into a tab host")
	}
	if inst.TabCount() != 2 {
		t.Fatalf("host tabs = %d, want 2 (preview + file)", inst.TabCount())
	}
	if c := inst.TabContent(0); c == nil || c.Kind() != pane.KindMarkdown {
		t.Fatal("the live preview must stay as the first tab")
	}
	if ed := inst.TabEditor(1); ed == nil || !ed.HasFile() {
		t.Fatal("the dragged file must join as a document tab")
	}
}

// TestContentTabDragsOutToEdge (#1778): a content tab dropped on the host's
// own edge zone splits back out into a dedicated viewer pane, live content
// intact.
func TestContentTabDragsOutToEdge(t *testing.T) {
	m, edKey, pvKey := mdPreviewModel(t)
	pr := m.lay.Panes[pvKey]
	m = step(m, press(pr.X+2, pr.Y+1))
	dst := m.lay.Panes[edKey]
	m = step(m, release(dst.X+dst.W/2, dst.Y+dst.H/2))
	inst := m.activeWS().Panes.Get(edKey)
	if inst == nil || inst.TabCount() != 2 {
		t.Fatal("setup: merge failed")
	}
	idx := -1
	for i := 0; i < inst.TabCount(); i++ {
		if inst.TabContent(i) != nil {
			idx = i
		}
	}
	r := m.lay.Panes[edKey]
	m.drag = &dragState{kind: dragTab, srcPane: edKey, srcTab: idx, startX: r.X + 2, startY: r.Y + 1, curX: r.X + 1, curY: r.Y + r.H/2}
	m.commitTabMove(r.X+1, r.Y+r.H/2)
	m.drag = nil

	if inst.TabCount() != 1 {
		t.Fatalf("host tabs after split-out = %d, want 1", inst.TabCount())
	}
	found := false
	for _, key := range m.activeWS().Panes.Keys() {
		if c := m.activeWS().Panes.Get(key); c != nil && c.Kind() == pane.KindMarkdown {
			found = true
			if c.Preview().Path() == "" {
				t.Fatal("the split-out preview must keep its binding")
			}
		}
	}
	if !found {
		t.Fatal("the content tab must become a dedicated preview pane again")
	}
}

// TestMixedTabStripPersists (#1778): a tab host holding a document and a
// preview content tab saves and restores mixed — the preview comes back as a
// tab in the same pane, not as a separate pane or nothing.
func TestMixedTabStripPersists(t *testing.T) {
	m, edKey, pvKey := mdPreviewModel(t)
	pr := m.lay.Panes[pvKey]
	m = step(m, press(pr.X+2, pr.Y+1))
	dst := m.lay.Panes[edKey]
	m = step(m, release(dst.X+dst.W/2, dst.Y+dst.H/2))
	inst := m.activeWS().Panes.Get(edKey)
	if inst == nil || inst.TabCount() != 2 {
		t.Fatal("setup: merge failed")
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)

	m2 := NewWith(registry.New(), host.MapConfig{})
	restored := m2.activeWS().Panes.Get(edKey)
	if restored == nil || restored.Kind() != pane.KindEditor {
		t.Fatalf("tab host did not restore under %q", edKey)
	}
	if restored.TabCount() != 2 {
		t.Fatalf("restored tabs = %d, want 2 (file + preview)", restored.TabCount())
	}
	cIdx := -1
	for i := 0; i < restored.TabCount(); i++ {
		if c := restored.TabContent(i); c != nil {
			cIdx = i
			if c.Kind() != pane.KindMarkdown {
				t.Fatalf("restored content tab kind = %d, want markdown", c.Kind())
			}
		}
	}
	if cIdx < 0 {
		t.Fatal("the preview content tab must restore")
	}
	if restored.ActiveTab() != cIdx {
		t.Fatalf("active tab = %d, want the content tab %d", restored.ActiveTab(), cIdx)
	}
	for _, leaf := range layout.Leaves(m2.activeWS().Tree) {
		if leaf == pvKey {
			t.Fatal("the merged preview must not restore as its own leaf")
		}
	}
}

// TestSingletonPaneStaysEdgeOnlyTarget (#1778): a dragged tab over a
// singleton tool window still resolves to edge zones only — no center merge,
// no silent conversion.
func TestSingletonPaneStaysEdgeOnlyTarget(t *testing.T) {
	m, edKey, pvKey := mdPreviewModel(t)
	_ = pvKey
	r, ok := m.lay.Panes[pane.ExplorerKey]
	if !ok {
		t.Fatal("setup: no explorer pane")
	}
	d := &dragState{kind: dragTab, srcPane: edKey, srcTab: 0, startX: 0, startY: 0, curX: r.X + r.W/2, curY: r.Y + r.H/2}
	if zone, can := m.dropZoneFor(d, pane.ExplorerKey, r); can && zone == layout.ZoneCenter {
		t.Fatal("a singleton pane must not offer the center merge zone")
	}
	if m.ensureTabHost(pane.ExplorerKey) {
		t.Fatal("a singleton pane must not convert into a tab host")
	}
	if m.activeWS().Panes.Get(pane.ExplorerKey).Kind() != pane.KindExplorer {
		t.Fatal("the explorer must stay an explorer")
	}
}

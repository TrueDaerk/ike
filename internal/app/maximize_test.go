package app

import (
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// maximize_test.go covers pane.maximize (#358): zoom rendering, exact
// restore, and the auto-unzoom-on-mutation invariant.

func TestMaximizeZoomsAndRestores(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	m := openApp(t, a)
	before := len(m.lay.Panes) // explorer + editor
	if before < 2 {
		t.Fatalf("setup: want at least 2 panes, got %d", before)
	}
	key := m.activeWS().Panes.Focused()
	origRect := m.lay.Panes[key]

	m = dispatch(t, m, MaximizePaneMsg{})
	if len(m.lay.Panes) != 1 {
		t.Fatalf("zoomed layout must hold exactly the focused pane, got %v", m.lay.Panes)
	}
	if r := m.lay.Panes[key]; r != m.bodyRect() {
		t.Fatalf("zoomed pane must own the body rect, got %+v", r)
	}
	if len(m.lay.Dividers) != 0 {
		t.Fatal("zoomed layout must have no dividers")
	}

	m = dispatch(t, m, MaximizePaneMsg{})
	if len(m.lay.Panes) != before {
		t.Fatalf("toggle must restore all panes, got %d", len(m.lay.Panes))
	}
	if r := m.lay.Panes[key]; r != origRect {
		t.Fatalf("restored rect %+v, want %+v", r, origRect)
	}
}

func TestMaximizeSplitWhileZoomedUnzooms(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	m := openApp(t, a)
	m = dispatch(t, m, MaximizePaneMsg{})
	if len(m.lay.Panes) != 1 {
		t.Fatal("setup: zoom must be active")
	}

	// A tree mutation (split) changes the leaf set: the zoom must drop and
	// every pane — including the new one — must be visible.
	m = dispatch(t, m, SplitFocusedMsg{Zone: layout.ZoneRight})
	if m.zoomed != "" {
		t.Fatal("split while zoomed must unzoom")
	}
	if len(m.lay.Panes) < 3 {
		t.Fatalf("all panes must be laid out after the split, got %v", m.lay.Panes)
	}
}

func TestMaximizeExplorerPaneToo(t *testing.T) {
	m := newSized()
	m.setFocus(pane.ExplorerKey)
	m.layout()
	m = dispatch(t, m, MaximizePaneMsg{})
	if len(m.lay.Panes) != 1 {
		t.Fatalf("any focused pane zooms, got %v", m.lay.Panes)
	}
	if _, ok := m.lay.Panes[pane.ExplorerKey]; !ok {
		t.Fatal("explorer must be the zoomed pane")
	}
}

func TestMaximizeCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("pane.maximize"); !ok {
		t.Fatal("pane.maximize must be registered")
	}
}

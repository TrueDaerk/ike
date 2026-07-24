package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/vcs"
)

// TestVCSPanelRestores guards the slimmed #750 panel: quitting with the panel
// open restores it in its slot, and the first status snapshot re-feeds it.
func TestVCSPanelRestores(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.vcs.snap = &vcs.Snapshot{Root: "/r", Branch: "main"}
	out, _ = m.Update(VCSPanelToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.VCSKey) {
		t.Fatal("setup: panel not open")
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes) // what quit() does

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(pane.VCSKey)
	if inst == nil || inst.Kind() != pane.KindVCS {
		t.Fatal("panel did not restore")
	}

	// The initial snapshot arrival re-feeds the restored panel.
	out, _ = m2.Update(vcs.SnapshotMsg{Snap: &vcs.Snapshot{Root: "/r", Branch: "main",
		Entries: []vcs.FileEntry{{Path: "a.go", Status: vcs.StatusModified, X: '.', Y: 'M'}}}})
	m2 = out.(Model)
	if v := m2.activeWS().Panes.Get(pane.VCSKey).VCS().View(); v == "" {
		t.Fatal("restored panel renders empty")
	}
}

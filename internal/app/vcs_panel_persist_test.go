package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/vcs"
	"ike/internal/vcspanel"
)

// TestVCSPanelTabRestores guards #504: quitting with the Log tab open
// restores the Log tab, and the first status snapshot triggers the log's
// initial window load.
func TestVCSPanelTabRestores(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.vcs.snap = &vcs.Snapshot{Root: "/r", Branch: "main"}
	out, _ = m.Update(VCSPanelToggleMsg{})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = out.(Model)
	if m.panes.Get(pane.VCSKey).VCS().ActiveTab() != vcspanel.TabLog {
		t.Fatal("setup: log tab not active")
	}
	saveLayout(m.tree, m.panes) // what quit() does

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.panes.Get(pane.VCSKey)
	if inst == nil || inst.Kind() != pane.KindVCS {
		t.Fatal("panel did not restore")
	}
	if inst.VCS().ActiveTab() != vcspanel.TabLog {
		t.Fatal("restored panel must reopen the Log tab")
	}

	// The initial snapshot arrival kicks the log's first window load.
	out, cmd := m2.Update(vcs.SnapshotMsg{Snap: &vcs.Snapshot{Root: "/r", Branch: "main"}})
	m2 = out.(Model)
	if cmd == nil {
		t.Fatal("snapshot must produce commands")
	}
	if !producesLogRequest(cmd) {
		t.Fatal("restored Log tab did not request its first window")
	}
}

// producesLogRequest walks a (possibly batched) command tree for a
// vcspanel.LogRequestMsg without running toast timers.
func producesLogRequest(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case vcspanel.LogRequestMsg:
		return true
	case tea.BatchMsg:
		for _, c := range msg {
			if producesLogRequest(c) {
				return true
			}
		}
	}
	return false
}

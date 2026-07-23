package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// explorerRect finds the explorer pane's rect.
func explorerRect(t *testing.T, m Model) layout.Rect {
	t.Helper()
	r, ok := m.lay.Panes[pane.ExplorerKey]
	if !ok {
		t.Fatal("no explorer rect")
	}
	return r
}

// TestRightClickOpensExplorerContextMenu guards #1040: a right-click on the
// tree opens the node context menu at the pointer; a left press outside
// dismisses it without leaking.
func TestRightClickOpensExplorerContextMenu(t *testing.T) {
	ran := false
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Commands: []plugin.Command{{
		ID: "explorer.refresh", Title: "Refresh",
		Run: func(h host.API) tea.Cmd { ran = true; return nil },
	}}}})
	m := sizedWith(t, reg, 100, 40)
	r := explorerRect(t, m)
	x, y := r.X+paneContentX+3, r.Y+paneContentY
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
	if !m.ctxMenu.IsOpen() {
		t.Fatal("right-click on the explorer must open the context menu")
	}
	// Find the Refresh row (the only runnable entry in this registry).
	px, py := m.ctxMenu.Pos()
	row := -1
	for i, it := range explorerContextItems() {
		if it.Command == "explorer.refresh" {
			row = i
		}
	}
	out, cmd := m.Update(tea.MouseClickMsg{X: px + 1, Y: py + 1 + row, Button: tea.MouseLeft})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("clicking the enabled entry must dispatch")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	if !ran {
		t.Fatal("explorer.refresh must run")
	}
	if m.ctxMenu.IsOpen() {
		t.Fatal("invoking must close the menu")
	}
}

package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/registry"
)

// TestRestoredLayoutKeepsThemePalette (#722): restoreLayout replaces the pane
// registry AFTER the startup applyTheme — without re-threading, restored
// panes (explorer file colors, editor captures) silently fall back to the
// default dark theme's tokens, which are near-unreadable on a light theme's
// background.
func TestRestoredLayoutKeepsThemePalette(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.py")
	os.WriteFile(file, []byte("import os\n"), 0o644)

	cfg := host.MapConfig{"theme.name": "catppuccin-latte"}
	m := NewWith(registry.New(), cfg)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.openPath(file, false)
	saveLayout(m.tree, m.panes)

	m2 := NewWith(registry.New(), cfg)
	p := m2.panes.Palette()
	if p == nil {
		t.Fatal("restored registry lost the palette entirely")
	}
	if p.Name != "catppuccin-latte" {
		t.Fatalf("restored registry palette = %q, want catppuccin-latte", p.Name)
	}
}

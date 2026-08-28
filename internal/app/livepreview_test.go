package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/registry"
	"ike/internal/settings"
)

// livepreview_test.go guards the app half of #2181: the settings theme picker
// browses by emitting debounce ticks, and the tick the root resolves has to
// re-theme everything already on screen — including the syntax spans of a
// buffer that is open and stays open.

// previewApp boots a sized model with one open Python buffer.
func previewApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	file := filepath.Join(dir, "a.py")
	if err := os.WriteFile(file, []byte("import os\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewWith(registry.New(), host.MapConfig{"theme.name": "default"})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.openPath(file, false)
	return m
}

// TestPreviewTickRethemesOpenBuffers guards #2181: a preview re-threads the
// palette through the live pane registry, so an already-open buffer re-colors
// its syntax spans without being reopened.
func TestPreviewTickRethemesOpenBuffers(t *testing.T) {
	m := previewApp(t)
	before := m.activeWS().Panes.Palette()
	if before == nil || before.Name != "default" {
		t.Fatalf("precondition: palette = %v, want default", before)
	}
	beforeKeyword := before.Captures["keyword"]

	out, _ := m.Update(settings.PreviewMsg{Key: "theme.name", Value: "catppuccin-latte"})
	m = out.(Model)

	after := m.activeWS().Panes.Palette()
	if after == nil || after.Name != "catppuccin-latte" {
		t.Fatalf("preview palette = %v, want catppuccin-latte", after)
	}
	if after.Captures["keyword"] == beforeKeyword {
		t.Fatal("previewing another theme must re-color the syntax captures of open buffers")
	}

	// The rollback the picker sends on esc puts the previous theme back.
	out, _ = m.Update(settings.PreviewMsg{Key: "theme.name", Value: "default"})
	m = out.(Model)
	if back := m.activeWS().Panes.Palette(); back == nil || back.Name != "default" {
		t.Fatalf("rollback palette = %v, want default", back)
	}
}

// TestPreviewTickRoutesToThePanel guards #2181: the debounce deadline reaches
// the settings panel, which decides whether it still applies.
func TestPreviewTickRoutesToThePanel(t *testing.T) {
	m := previewApp(t)
	// No browse is in flight, so an orphaned deadline is dropped silently
	// rather than re-theming anything.
	out, cmd := m.Update(settings.PreviewTickMsg{Key: "theme.name", Gen: 7})
	m = out.(Model)
	if cmd != nil {
		t.Fatalf("an orphaned deadline must produce no command, got %+v", cmd())
	}
	if p := m.activeWS().Panes.Palette(); p == nil || p.Name != "default" {
		t.Fatalf("palette = %v, want an untouched default", p)
	}
}

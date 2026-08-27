package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/frecency"
	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/registry"
)

// TestOpenFileBumpsFrecency guards #2155's event source: opening a file — the
// same event the recent-files MRU records — raises its frecency and persists
// the store under the project's state directory, so the '@' finder ranks it
// higher in the next session too.
func TestOpenFileBumpsFrecency(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	path := filepath.Join(t.TempDir(), "hot.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Build the model directly rather than through sized(): that helper
	// redirects IKE_CONFIG_DIR to a temp dir of its own, and this test needs
	// the store to land in the directory it checks.
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	key := frecency.Key(path)
	if got := m.fileFrec.Score(key); got != 0 {
		t.Fatalf("fresh store must be cold, score = %v", got)
	}

	out, _ = m.Update(palette.OpenFileMsg{Path: path})
	m = out.(Model)

	if got := m.fileFrec.Score(key); got <= 0 {
		t.Fatalf("opening a file must record its frecency, score = %v", got)
	}
	store := filepath.Join(dir, "filefrecency.json")
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("frecency store must persist per project: %v", err)
	}
	if got := palette.LoadFileFrecency(store).Score(key); got <= 0 {
		t.Fatalf("persisted store lost the event, score = %v", got)
	}
}

// TestFrecencyStorePathRedirects guards the state-file seam: the store follows
// IKE_CONFIG_DIR like every other per-project state file, and falls back to the
// project's own .ike directory.
func TestFrecencyStorePathRedirects(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", "/tmp/ike-state")
	if got, want := fileFrecencyFile(), filepath.Join("/tmp/ike-state", "filefrecency.json"); got != want {
		t.Fatalf("fileFrecencyFile() = %q, want %q", got, want)
	}
	t.Setenv("IKE_CONFIG_DIR", "")
	if got, want := fileFrecencyFile(), filepath.Join(".ike", "filefrecency.json"); got != want {
		t.Fatalf("fileFrecencyFile() = %q, want %q", got, want)
	}
}

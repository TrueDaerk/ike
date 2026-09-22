package pane

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/host"
)

// nbconfig_test.go guards the notebook.image_max_cols wiring (#2683): the
// persisted cap reaches a freshly opened notebook pane, and a config reload
// reaches the ones already open so their image placements resize.

// tempNotebook writes a minimal notebook and returns its path.
func tempNotebook(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "n.ipynb")
	doc := `{"cells": [], "metadata": {}, "nbformat": 4, "nbformat_minor": 5}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNotebookPaneTakesImageCapFromConfig(t *testing.T) {
	r := NewRegistry(host.MapConfig{"notebook.image_max_cols": "40"}, nil)
	key := r.AddNotebookView(tempNotebook(t))
	if got := r.Get(key).Notebook().ImageMaxCols(); got != 40 {
		t.Fatalf("image cap = %d, want the persisted 40", got)
	}
}

func TestNotebookPaneDefaultsImageCapWithoutKey(t *testing.T) {
	r := NewRegistry(host.MapConfig{}, nil)
	key := r.AddNotebookView(tempNotebook(t))
	if got := r.Get(key).Notebook().ImageMaxCols(); got != defaultNotebookImageMaxCols {
		t.Fatalf("image cap = %d, want the default %d", got, defaultNotebookImageMaxCols)
	}
}

func TestNotebookPaneFollowsConfigReload(t *testing.T) {
	r := NewRegistry(host.MapConfig{}, nil)
	key := r.AddNotebookView(tempNotebook(t))
	inst := r.Get(key)
	r.Reconfigure(host.MapConfig{"notebook.image_max_cols": "0"})
	if got := inst.Notebook().ImageMaxCols(); got != 0 {
		t.Fatalf("image cap = %d after the reload, want the cap lifted", got)
	}
	r.Reconfigure(host.MapConfig{"notebook.image_max_cols": "120"})
	if got := inst.Notebook().ImageMaxCols(); got != 120 {
		t.Fatalf("image cap = %d after the reload, want 120", got)
	}
}

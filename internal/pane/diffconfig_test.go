package pane

import (
	"testing"

	"ike/internal/host"
)

// diffconfig_test.go guards the diff.* config wiring (#2170): the persisted
// ignore-whitespace preference reaches a fresh diff pane, and a reload reaches
// the ones already open — the viewer's own w toggle writes the key back, so
// both directions have to work.

func TestDiffPaneTakesIgnoreWhitespaceFromConfig(t *testing.T) {
	r := NewRegistry(host.MapConfig{"diff.ignore_whitespace": "true", "diff.context": "5"}, nil)
	key := r.AddDiff("/tmp/a.txt", "/tmp/b.txt")
	if !r.Get(key).Diff().IgnoreWhitespace() {
		t.Fatal("a fresh diff pane should start with the persisted preference")
	}
}

func TestDiffPaneFollowsConfigReload(t *testing.T) {
	r := NewRegistry(host.MapConfig{}, nil)
	key := r.AddDiff("/tmp/a.txt", "/tmp/b.txt")
	inst := r.Get(key)
	inst.Diff().SetContents("a\n\tb\n", "a\n    b\n")
	if inst.Diff().HunkCount() != 1 {
		t.Fatalf("expected the re-indentation as one hunk, got %d", inst.Diff().HunkCount())
	}
	r.Reconfigure(host.MapConfig{"diff.ignore_whitespace": "true"})
	if !inst.Diff().IgnoreWhitespace() {
		t.Fatal("a reload should re-apply the preference to the open pane")
	}
	if n := inst.Diff().HunkCount(); n != 0 {
		t.Fatalf("the whitespace-only hunk should be gone after the reload, got %d", n)
	}
}

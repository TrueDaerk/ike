package app

import (
	"testing"
	"time"

	"ike/internal/config"
	"ike/internal/project"
)

// history_prune_test.go covers #842: the picker aux action removes a history
// entry, the config reloads and the open palette re-lists without it.

func TestRemoveFromHistoryEndToEnd(t *testing.T) {
	restore := config.Get()
	t.Cleanup(func() { config.Set(restore) })
	m := sized(t, 100, 40)
	rootA, rootB := t.TempDir(), t.TempDir()
	opts := config.Discover(".")
	if err := project.RecordOpen(opts, rootA, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := project.RecordOpen(opts, rootB, time.Now()); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(opts)
	config.Set(cfg)
	if len(project.History(config.Get())) != 2 {
		t.Fatal("setup: two history entries expected")
	}

	// The aux msg runs the off-loop write and reports back.
	out, cmd := m.Update(project.RemoveFromHistoryMsg{Path: rootA})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("RemoveFromHistoryMsg must return the write command")
	}
	removed, ok := cmd().(project.RemovedFromHistoryMsg)
	if !ok || removed.Err != nil {
		t.Fatalf("write result = %#v", removed)
	}
	out, cmd = m.Update(removed)
	m = out.(Model)
	if cmd == nil {
		t.Fatal("a successful removal must trigger the config reload")
	}
	reloaded, ok := cmd().(config.ConfigReloadedMsg)
	if !ok {
		t.Fatalf("expected ConfigReloadedMsg, got %#v", reloaded)
	}
	out, _ = m.Update(reloaded)
	m = out.(Model)

	h := project.History(config.Get())
	if len(h) != 1 || h[0].Path != rootB {
		t.Fatalf("history after removal = %+v, want only %s", h, rootB)
	}
	_ = m
}

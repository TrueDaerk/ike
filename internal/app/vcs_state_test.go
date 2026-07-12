package app

import (
	"testing"

	"ike/internal/host"
	"ike/internal/registry"
	"ike/internal/vcs"
	"ike/internal/watch"
)

func vcsApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	return NewWith(registry.New(), host.MapConfig{})
}

func TestVCSWatcherEventArmsDebounce(t *testing.T) {
	m := vcsApp(t)
	_, cmd := m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: "x.go"})
	if cmd == nil || !m.vcs.tickArmed {
		t.Fatalf("watcher event must arm the vcs debounce tick (cmd=%v armed=%v)", cmd, m.vcs.tickArmed)
	}
	// A second event while armed must not arm another tick.
	if c := m.scheduleVCSRefresh(); c != nil {
		t.Fatal("second trigger while armed must not schedule again")
	}
}

func TestVCSSaveInvalidateArmsDebounce(t *testing.T) {
	m := vcsApp(t)
	if _, cmd := m.Update(vcsInvalidateMsg{}); cmd == nil || !m.vcs.tickArmed {
		t.Fatal("invalidate must arm the vcs debounce tick")
	}
}

func TestVCSTickRunsSerializedRefresh(t *testing.T) {
	m := vcsApp(t)
	m.vcs.tickArmed = true
	if _, cmd := m.Update(vcsTickMsg{}); cmd == nil {
		t.Fatal("tick must launch the refresh")
	}
	if m.vcs.tickArmed || !m.vcs.refreshing {
		t.Fatalf("after tick: armed=%v refreshing=%v", m.vcs.tickArmed, m.vcs.refreshing)
	}
	// A tick arriving mid-flight queues exactly one follow-up instead of a
	// second subprocess.
	if _, cmd := m.Update(vcsTickMsg{}); cmd != nil {
		t.Fatal("mid-flight tick must not launch a second refresh")
	}
	if !m.vcs.dirty {
		t.Fatal("mid-flight tick must mark the state dirty")
	}

	snap := &vcs.Snapshot{Root: "/r", Branch: "main"}
	_, cmd := m.Update(vcs.SnapshotMsg{Snap: snap})
	if m.VCSSnapshot() != snap {
		t.Fatal("snapshot not stored")
	}
	if cmd == nil || !m.vcs.refreshing || m.vcs.dirty {
		t.Fatalf("dirty state must chain a follow-up refresh (cmd=%v refreshing=%v dirty=%v)",
			cmd, m.vcs.refreshing, m.vcs.dirty)
	}
	// The follow-up completes with no further trigger: the chain stops.
	if _, cmd := m.Update(vcs.SnapshotMsg{Snap: nil}); cmd != nil {
		t.Fatal("clean completion must not refresh again")
	}
	if m.VCSSnapshot() != nil {
		t.Fatal("nil snapshot (not a repo) must replace the old one")
	}
}

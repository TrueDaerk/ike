package workspace

import (
	"testing"

	"ike/internal/terminal"
)

// globaltools_test.go — the manager's parked global tool stash (#1890):
// detached global tool sessions live on the manager, keyed by tool name, so
// they survive model rebuilds and belong to no workspace registry.

func TestManagerGlobalToolStash(t *testing.T) {
	m := NewManager(New("/a", nil))
	if _, ok := m.TakeGlobalTool("sql"); ok {
		t.Fatal("empty stash must not return a session")
	}
	var sess terminal.Model
	sess.SetTool("sql")
	m.ParkGlobalTool("sql", sess)
	if got := m.GlobalToolNames(); len(got) != 1 || got[0] != "sql" {
		t.Fatalf("GlobalToolNames = %v, want [sql]", got)
	}
	taken, ok := m.TakeGlobalTool("sql")
	if !ok || taken.Tool() != "sql" {
		t.Fatalf("TakeGlobalTool = %v/%v, want the parked session", taken.Tool(), ok)
	}
	if _, ok := m.TakeGlobalTool("sql"); ok {
		t.Fatal("Take must remove the entry")
	}
	if got := m.GlobalToolNames(); len(got) != 0 {
		t.Fatalf("stash must be empty after Take, got %v", got)
	}
}

func TestManagerGlobalToolNamesSorted(t *testing.T) {
	m := NewManager(New("/a", nil))
	for _, name := range []string{"zeta", "alpha", "mid"} {
		m.ParkGlobalTool(name, terminal.Model{})
	}
	got := m.GlobalToolNames()
	want := []string{"alpha", "mid", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GlobalToolNames = %v, want %v", got, want)
		}
	}
}

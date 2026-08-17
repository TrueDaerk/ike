package settings

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// run_page_test.go guards the Run page (#1905): run.placement names the Run
// tool's home position, so its option set must be the tool home positions
// plus in_pane — and editing it must persist and render like any enum.

// runPlacementEntry returns the shipped run.placement schema entry.
func runPlacementEntry(t *testing.T) Entry {
	t.Helper()
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		for _, e := range p.Entries {
			if e.Key == "run.placement" {
				return e
			}
		}
	}
	t.Fatal("the schema must expose run.placement")
	return Entry{}
}

// TestRunPlacementOptions: the enum offers exactly the Run tool's placements.
func TestRunPlacementOptions(t *testing.T) {
	e := runPlacementEntry(t)
	want := []string{"bottom", "left", "right", "top", "in_pane"}
	if len(e.Options) != len(want) {
		t.Fatalf("run.placement options = %v, want %v", e.Options, want)
	}
	for i, v := range want {
		if e.Options[i] != v {
			t.Fatalf("run.placement options = %v, want %v", e.Options, want)
		}
	}
}

// TestRunPlacementRoundTrip: setting the placement through the panel persists
// it, the reloaded config reads it back, and the entry row shows the value.
func TestRunPlacementRoundTrip(t *testing.T) {
	restoreConfig(t)
	e := runPlacementEntry(t)
	m := New([]Page{{Title: "Run", Entries: []Entry{e}}}, testOpts(t))
	m.SetSize(90, 24)
	m.Open()

	m.writeValue(e, "left")
	commit(t, m)
	if got := config.Get().Run.Placement; got != "left" {
		t.Fatalf("persisted run.placement = %q, want left", got)
	}
	if got := m.value("run.placement"); got != "left" {
		t.Fatalf("panel value = %q, want left", got)
	}
	if v := m.View(); !strings.Contains(v, "left") {
		t.Fatalf("the entry row must render the value:\n%s", v)
	}

	// An invalid value never reaches the config: validation snaps it back to
	// the shipped default.
	m.writeValue(e, "sideways")
	commit(t, m)
	if got := config.Get().Run.Placement; got != "bottom" {
		t.Fatalf("rejected run.placement = %q, want the bottom default", got)
	}
}

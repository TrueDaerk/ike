package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/vcs"
)

func TestBranchModeRanksCurrentFirst(t *testing.T) {
	mode := newBranchMode(func() []vcs.Branch {
		return []vcs.Branch{
			{Name: "feature/x"},
			{Name: "main", Current: true},
		}
	})
	items := mode.Results("", palette.Context{})
	if len(items) != 2 || items[0].Title != "main" || items[0].Detail != "current" {
		t.Fatalf("items = %+v", items)
	}
	if _, ok := items[0].Msg.(CheckoutBranchMsg); !ok {
		t.Fatalf("item msg = %T", items[0].Msg)
	}
	// Fuzzy narrows.
	items = mode.Results("feat", palette.Context{})
	if len(items) != 1 || items[0].Title != "feature/x" {
		t.Fatalf("filtered = %+v", items)
	}
}

func TestBranchPickerOpensOnBranchesMsg(t *testing.T) {
	m := vcsApp(t)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)

	// No repo: hint, picker stays closed.
	out, _ = m.Update(OpenBranchPickerMsg{})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("picker must not open without a repo")
	}

	m.vcs.snap = &vcs.Snapshot{Root: "/r", Branch: "main"}
	if _, cmd := m.Update(OpenBranchPickerMsg{}); cmd == nil {
		t.Fatal("picker must fetch branches")
	}
	out, _ = m.Update(vcs.BranchesMsg{Branches: []vcs.Branch{{Name: "main", Current: true}}})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("branch list must open the palette")
	}
	if len(m.vcs.branches) != 1 {
		t.Fatal("branches not parked on the shared state")
	}

	// Selecting the current branch is a no-op hint; another branch checks out.
	out, cmd := m.Update(CheckoutBranchMsg{Name: "main"})
	m = out.(Model)
	_ = cmd
	out, cmd = m.Update(CheckoutBranchMsg{Name: "dev"})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("selecting another branch must launch the checkout")
	}
}

func TestDiffHeadOpensDiffPane(t *testing.T) {
	m := vcsApp(t)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	dir := t.TempDir()
	path := writeTemp(t, dir, "f.go", "live\n")
	tm, _ := m.openPath(path, false)
	m = tm.(Model)

	// Untracked: hint, no pane.
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{"f.go": vcs.StatusUntracked})
	out, _ = m.Update(DiffHeadMsg{})
	m = out.(Model)

	// Tracked: HEAD content arrives, the diff pane opens focused.
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{"f.go": vcs.StatusModified})
	if _, cmd := m.Update(DiffHeadMsg{}); cmd == nil {
		t.Fatal("tracked file must fetch the HEAD blob")
	}
	out, _ = m.Update(vcs.HeadDiffMsg{Path: path, Head: "old\n"})
	m = out.(Model)
	inst := m.panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatalf("focused pane = %v, want diff", m.panes.Focused())
	}
	if hc := inst.Diff().HunkCount(); hc != 1 {
		t.Fatalf("hunks = %d, want 1 (old vs live)", hc)
	}
}

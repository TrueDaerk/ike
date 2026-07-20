package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/vcs"
	"ike/internal/vcspanel"
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

func TestVCSPanelToggleLifecycle(t *testing.T) {
	m := vcsApp(t)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)

	// Outside a repo: hint, no pane.
	out, _ = m.Update(VCSPanelToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Has(pane.VCSKey) {
		t.Fatal("panel must not open without a repo")
	}

	m.vcs.snap = &vcs.Snapshot{Root: "/r", Branch: "main"}
	before := m.activeWS().Panes.Focused()
	out, _ = m.Update(VCSPanelToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.VCSKey) || m.activeWS().Panes.Focused() != pane.VCSKey {
		t.Fatalf("first toggle must open + focus the panel (focus=%q)", m.activeWS().Panes.Focused())
	}

	// Second toggle returns focus whence it came.
	out, _ = m.Update(VCSPanelToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}
	// Third re-focuses the existing pane without duplicating it.
	out, _ = m.Update(VCSPanelToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.VCSKey {
		t.Fatal("third toggle must re-focus the panel")
	}

	// A snapshot refresh reaches the panel.
	snap := &vcs.Snapshot{Root: "/r", Branch: "dev"}
	out, _ = m.Update(vcs.SnapshotMsg{Snap: snap})
	m = out.(Model)
	if !strings.Contains(stripped(m), "⎇ dev") {
		t.Fatal("panel header should carry the refreshed branch")
	}
}

func TestVCSPanelSharesDraftWithDialog(t *testing.T) {
	m := vcsApp(t)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.vcs.snap = &vcs.Snapshot{Root: "/r", Branch: "main",
		Entries: []vcs.FileEntry{{Path: "a.go", Status: vcs.StatusModified, X: 'M', Y: '.'}}}
	out, _ = m.Update(VCSPanelToggleMsg{})
	m = out.(Model)

	// Type a message in the panel; the modal dialog sees the same draft.
	out, _ = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = out.(Model)
	for _, r := range "wip" {
		out, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	if m.commitUI.Message() != "wip" {
		t.Fatalf("dialog draft = %q, want the panel's text", m.commitUI.Message())
	}

	// Panel commit request routes to git; a successful commit clears the
	// shared draft for both entry points.
	out, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("panel ctrl+s must submit")
	}
	if _, ok := cmd().(vcspanel.SubmitMsg); !ok {
		t.Fatalf("submit msg = %T", cmd())
	}
	out, _ = m.Update(vcs.CommitDoneMsg{Hash: "abc", Summary: "wip"})
	m = out.(Model)
	if m.commitUI.Message() != "" || m.vcs.draft.Text != "" {
		t.Fatal("commit success must clear the shared draft")
	}
}

func TestVCSPanelLogRouting(t *testing.T) {
	m := vcsApp(t)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.vcs.snap = &vcs.Snapshot{Root: "/r", Branch: "main"}
	out, _ = m.Update(VCSPanelToggleMsg{})
	m = out.(Model)

	// Selecting the Log tab requests the first window through the app.
	out, cmd := m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("log tab must request a window")
	}
	if _, ok := cmd().(vcspanel.LogRequestMsg); !ok {
		t.Fatalf("cmd msg = %T", cmd())
	}
	// The loaded window lands in the panel; a commit-file diff opens a pane
	// with the parent/commit contents.
	out, _ = m.Update(vcs.LogMsg{Entries: []vcs.LogEntry{{Hash: strings.Repeat("a", 40), ShortHash: "aaaaaaa", Author: "t", Subject: "one"}}})
	m = out.(Model)
	if !strings.Contains(stripped(m), "one") {
		t.Fatal("log entry missing from the panel")
	}
	panelW := m.lay.Panes[pane.VCSKey].W
	out, _ = m.Update(vcs.FileAtMsg{Hash: strings.Repeat("a", 40), Path: "f.txt", Parent: "v1\n", Content: "v2\n"})
	m = out.(Model)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindDiff || inst.Diff().HunkCount() != 1 {
		t.Fatalf("commit diff pane not opened (focus=%v)", m.activeWS().Panes.Focused())
	}
	// The diff splits the editor area, never the bottom tool window (#489):
	// the panel keeps its full width.
	if got := m.lay.Panes[pane.VCSKey].W; got != panelW {
		t.Fatalf("panel width %d → %d: diff carved the tool window", panelW, got)
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
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatalf("focused pane = %v, want diff", m.activeWS().Panes.Focused())
	}
	if hc := inst.Diff().HunkCount(); hc != 1 {
		t.Fatalf("hunks = %d, want 1 (old vs live)", hc)
	}
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
	"ike/internal/vcs"
	"ike/internal/watch"
)

// openFileDiff opens a file diff over two temp files and returns the model and
// the two paths.
func openFileDiff(t *testing.T, leftText, rightText string) (Model, string, string) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	if err := os.WriteFile(left, []byte(leftText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(right, []byte(rightText), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m.openDiffPane(left, right)
	return m, left, right
}

// focusedDiff returns the focused pane's diff model.
func focusedDiff(t *testing.T, m Model) *pane.Instance {
	t.Helper()
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatalf("focused pane is not a diff: %v", inst)
	}
	return inst
}

// TestDiffReloadsOnWatchEvent guards #2506: an external change to either side
// of an open file diff re-reads that side and re-diffs in place.
func TestDiffReloadsOnWatchEvent(t *testing.T) {
	m, left, right := openFileDiff(t, "a\nb\nc\n", "a\nb\nc\n")
	if got := focusedDiff(t, m).Diff().HunkCount(); got != 0 {
		t.Fatalf("setup: identical files have %d hunks", got)
	}

	if err := os.WriteFile(right, []byte("a\nB\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: right})
	m = tm.(Model)
	if got := focusedDiff(t, m).Diff().HunkCount(); got != 1 {
		t.Fatalf("after the right file changed: hunks = %d, want 1", got)
	}

	// The left side follows the same route.
	if err := os.WriteFile(left, []byte("a\nB\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: left})
	m = tm.(Model)
	if got := focusedDiff(t, m).Diff().HunkCount(); got != 0 {
		t.Fatalf("after both files matched: hunks = %d, want 0", got)
	}
}

// TestDiffReloadKeepsScrollAndHunk guards the "no reload jolt" half of #2506:
// the reader keeps their place across an external change.
func TestDiffReloadKeepsScrollAndHunk(t *testing.T) {
	var l, r strings.Builder
	for i := 0; i < 200; i++ {
		l.WriteString("line\n")
		r.WriteString("line\n")
	}
	m, _, right := openFileDiff(t, l.String(), "X\n"+r.String())
	d := focusedDiff(t, m).Diff()
	d.StepHunk(1)
	d.ScrollBy(40)
	hunk := d.CurrentHunk()

	if err := os.WriteFile(right, []byte("X\n"+r.String()+"tail\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: right})
	m = tm.(Model)
	d = focusedDiff(t, m).Diff()
	if got := d.CurrentHunk(); got != hunk {
		t.Fatalf("current hunk = %d, want the retained %d", got, hunk)
	}
	if !strings.Contains(d.View(), "line") {
		t.Fatal("the reloaded diff renders nothing")
	}
}

// TestDiffRemovedSideShowsNotice guards #2506: a deleted file leaves an empty
// side plus a footer notice — no error dialog — and writing it again brings
// the content back.
func TestDiffRemovedSideShowsNotice(t *testing.T) {
	m, left, _ := openFileDiff(t, "one\ntwo\n", "one\ntwo\n")
	if err := os.Remove(left); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: left})
	m = tm.(Model)
	d := focusedDiff(t, m).Diff()
	if got := d.Notice(); got != "left file removed" {
		t.Fatalf("notice = %q, want %q", got, "left file removed")
	}
	if d.HunkCount() != 1 {
		t.Fatalf("a removed left side must diff as empty (hunks = %d)", d.HunkCount())
	}

	if err := os.WriteFile(left, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.Update(watch.EventMsg{Kind: watch.FileCreated, Path: left})
	m = tm.(Model)
	d = focusedDiff(t, m).Diff()
	if got := d.Notice(); got != "" {
		t.Fatalf("the restored file must clear the notice, got %q", got)
	}
	if d.HunkCount() != 0 {
		t.Fatalf("the restored content did not come back (hunks = %d)", d.HunkCount())
	}
}

// TestDiffSnapshotsDoNotReload guards the scope of #2506: a HEAD diff keeps
// its snapshot semantics when the working-tree file changes under it.
func TestDiffSnapshotsDoNotReload(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "r.txt")
	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{"r.txt": vcs.StatusModified})
	tm, _ := m.Update(vcs.HeadDiffMsg{Path: path, Head: "old\n"})
	m = tm.(Model)
	d := focusedDiff(t, m).Diff()
	if lr, _ := d.Revs(); lr != "HEAD" {
		t.Fatalf("setup: left rev = %q", lr)
	}
	before := d.HunkCount()

	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
	m = tm.(Model)
	if got := focusedDiff(t, m).Diff().HunkCount(); got != before {
		t.Fatalf("a HEAD diff must keep its snapshot: hunks %d → %d", before, got)
	}
}

// TestDiffWatchRegistersAndDropsPaths guards the no-leak rule of #2506: an
// open file diff holds a per-path watch for both its sides, retargeting swaps
// them, and closing the pane releases them.
func TestDiffWatchRegistersAndDropsPaths(t *testing.T) {
	m, left, right := openFileDiff(t, "a\n", "b\n")
	other := filepath.Join(filepath.Dir(left), "o.txt")
	if err := os.WriteFile(other, []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The registrations settle on the first Update pass after the open.
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	if got := m.watcher.WatchedPaths(); len(got) != 2 {
		t.Fatalf("WatchedPaths = %v, want both diff sides", got)
	}

	// Retargeting the single diff window swaps one side for another.
	m.openDiffPane(left, other)
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 31})
	m = tm.(Model)
	got := m.watcher.WatchedPaths()
	if len(got) != 2 || !containsPath(got, other) || containsPath(got, right) {
		t.Fatalf("after retarget WatchedPaths = %v, want [%s %s]", got, left, other)
	}

	// Closing the pane releases everything.
	m.closePane(m.activeWS().Panes.Focused())
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	m = tm.(Model)
	if got := m.watcher.WatchedPaths(); len(got) != 0 {
		t.Fatalf("a closed diff leaked %v", got)
	}
}

func containsPath(paths []string, want string) bool {
	abs := absDiffPath(want)
	for _, p := range paths {
		if p == abs {
			return true
		}
	}
	return false
}

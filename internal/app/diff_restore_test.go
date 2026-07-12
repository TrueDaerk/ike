package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/vcs"
)

// TestDiffLayoutRestores guards #490: a saved layout containing a diff pane
// restores intact — the validation pass used to reject the "diff" identity
// and silently drop the whole layout back to the default.
func TestDiffLayoutRestores(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(left, []byte("a\n"), 0o644)
	os.WriteFile(right, []byte("b\n"), 0o644)

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.openDiffPane(left, right)
	key := m.panes.Focused()
	if inst := m.panes.Get(key); inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatalf("setup: focused = %q", key)
	}
	saveLayout(m.tree, m.panes)

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatalf("diff pane did not restore under %q", key)
	}
	if inst.Diff().HunkCount() != 1 {
		t.Fatalf("restored diff hunks = %d, want 1 (contents re-read)", inst.Diff().HunkCount())
	}
	found := false
	for _, leaf := range layout.Leaves(m2.tree) {
		if leaf == key {
			found = true
		}
	}
	if !found {
		t.Fatal("diff leaf missing from the restored tree")
	}
}

// TestHeadDiffRestoresBothSides guards #508: a HEAD-vs-worktree diff pane
// survives a restart with the HEAD blob re-read via git instead of an empty
// left side.
func TestHeadDiffRestoresBothSides(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("v1\n"), 0o644)
	run("add", "f.txt")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")
	os.WriteFile(path, []byte("v2\n"), 0o644)

	// The restore path resolves the repo from the working directory.
	old, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(old) })

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.vcs.snap = &vcs.Snapshot{Root: dir, Branch: "main"}
	out, _ = m.Update(vcs.HeadDiffMsg{Path: path, Head: "v1\n"})
	m = out.(Model)
	key := m.panes.Focused()
	if m.panes.Get(key).Diff().HunkCount() != 1 {
		t.Fatal("setup: head diff has no hunk")
	}
	saveLayout(m.tree, m.panes)

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatal("head diff pane did not restore")
	}
	if got := inst.Diff().HunkCount(); got != 1 {
		t.Fatalf("restored hunks = %d, want 1 (left side must be the HEAD blob)", got)
	}
	if lr, rr := inst.Diff().Revs(); lr != "HEAD" || rr != "" {
		t.Fatalf("restored revs = %q/%q", lr, rr)
	}
	if !inst.Diff().Editable() {
		t.Fatal("restored head diff must stay editable")
	}
}

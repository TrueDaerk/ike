package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
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

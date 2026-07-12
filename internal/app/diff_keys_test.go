package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
	"ike/internal/vcs"
)

// TestDiffReopenFocusesExisting guards #509: opening the same diff again
// focuses the existing pane instead of splitting a duplicate.
func TestDiffReopenFocusesExisting(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(left, []byte("a\n"), 0o644)
	os.WriteFile(right, []byte("b\n"), 0o644)

	m := newSized()
	m.openDiffPane(left, right)
	key := m.panes.Focused()
	m.setFocus(pane.ExplorerKey)
	count := len(m.panes.Keys())

	m.openDiffPane(left, right)
	if len(m.panes.Keys()) != count {
		t.Fatal("re-open must not create a second pane")
	}
	if m.panes.Focused() != key {
		t.Fatalf("focus = %q, want the existing diff %q", m.panes.Focused(), key)
	}

	// A HEAD diff of the same file also dedupes.
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{"r.txt": vcs.StatusModified})
	out, _ := m.Update(vcs.HeadDiffMsg{Path: right, Head: "old\n"})
	m2 := out.(Model)
	headKey := m2.panes.Focused()
	m2.setFocus(pane.ExplorerKey)
	count = len(m2.panes.Keys())
	out, _ = m2.Update(vcs.HeadDiffMsg{Path: right, Head: "old\n"})
	m2 = out.(Model)
	if len(m2.panes.Keys()) != count || m2.panes.Focused() != headKey {
		t.Fatalf("head diff re-open: panes=%d focus=%q want %q", len(m2.panes.Keys()), m2.panes.Focused(), headKey)
	}
}

// TestDiffF7StepsHunks guards #495: F7 / shift+F7 drive the focused diff
// pane's hunk navigation through the diff-scoped default bindings.
func TestDiffF7StepsHunks(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(left, []byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"), 0o644)
	os.WriteFile(right, []byte("A\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nL\n"), 0o644)

	m := newSized()
	m.openDiffPane(left, right)
	inst := m.panes.FocusedInstance()
	if inst.Kind() != pane.KindDiff || inst.Diff().HunkCount() != 2 {
		t.Fatalf("setup: kind=%v hunks=%d", inst.Kind(), inst.Diff().HunkCount())
	}

	// The chord resolves to a command whose Run dispatches the step message;
	// run the returned command chain like the program loop would.
	press := func(k tea.KeyPressMsg) {
		t.Helper()
		out, cmd := m.Update(k)
		m = out.(Model)
		for cmd != nil {
			msg := cmd()
			if msg == nil {
				return
			}
			out, cmd = m.Update(msg)
			m = out.(Model)
		}
	}

	press(tea.KeyPressMsg{Code: tea.KeyF7})
	if got := m.panes.FocusedInstance().Diff().CurrentHunk(); got != 0 {
		t.Fatalf("after F7: hunk = %d, want 0", got)
	}
	press(tea.KeyPressMsg{Code: tea.KeyF7})
	if got := m.panes.FocusedInstance().Diff().CurrentHunk(); got != 1 {
		t.Fatalf("after F7 F7: hunk = %d, want 1", got)
	}
	press(tea.KeyPressMsg{Code: tea.KeyF7, Mod: tea.ModShift})
	if got := m.panes.FocusedInstance().Diff().CurrentHunk(); got != 0 {
		t.Fatalf("after shift+F7: hunk = %d, want 0", got)
	}
}

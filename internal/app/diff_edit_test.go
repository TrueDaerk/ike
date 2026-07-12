package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
	"ike/internal/vcs"
)

// chain feeds one command's message back into Update and stops: deeper
// levels are toast timers (tea.Tick sleeps when invoked synchronously).
func chain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	if msg := cmd(); msg != nil {
		out, _ := m.Update(msg)
		m = out.(Model)
	}
	return m
}

// TestDiffEditMode guards #496: 'e' on a worktree-backed diff mounts a live
// editor as the right column, edits re-diff live, ctrl+e returns to browsing.
func TestDiffEditMode(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644)

	m := newSized()
	// Worktree-backed HEAD diff: head text differs on line 1.
	m.vcs.snap = vcs.NewSnapshot(dir, map[string]vcs.FileStatus{"f.txt": vcs.StatusModified})
	out, _ := m.Update(vcs.HeadDiffMsg{Path: path, Head: "ONE\ntwo\nthree\n"})
	m = out.(Model)
	inst := m.panes.FocusedInstance()
	if inst.Kind() != pane.KindDiff || !inst.Diff().Editable() {
		t.Fatalf("setup: kind=%v editable=%v", inst.Kind(), inst.Diff().Editable())
	}

	// 'e' mounts the editor.
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m = out.(Model)
	m = chain(t, m, cmd)
	inst = m.panes.FocusedInstance()
	ed := inst.DiffEditor()
	if ed == nil || ed.Path() != path {
		t.Fatal("edit mode did not mount the editor")
	}
	hunks := inst.Diff().HunkCount()

	// Type: append a line at the top (insert mode 'O' + text + esc).
	for _, k := range []tea.KeyPressMsg{
		{Code: 'O', Text: "O"},
		{Code: 'x', Text: "x"},
		{Code: tea.KeyEscape},
	} {
		out, _ := m.Update(k)
		m = out.(Model)
	}
	inst = m.panes.FocusedInstance()
	if inst.Diff().HunkCount() == hunks && inst.DiffEditor().Text() == "one\ntwo\nthree\n" {
		t.Fatal("edit did not reach the embedded editor / re-diff")
	}

	// ctrl+e returns to browsing; the last state stays diffed.
	out, _ = m.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m = out.(Model)
	if m.panes.FocusedInstance().DiffEditor() != nil {
		t.Fatal("ctrl+e must leave edit mode")
	}
}

// TestDiffEditModeReadOnlyForRevisions guards the log diff: 'e' hints and
// mounts nothing.
func TestDiffEditModeReadOnlyForRevisions(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	m.vcs.snap = &vcs.Snapshot{Root: "/r", Branch: "main"}
	out, _ := m.Update(vcs.FileAtMsg{Hash: "aaaaaaaa", Path: "f.txt", Parent: "v1\n", Content: "v2\n"})
	m = out.(Model)
	inst := m.panes.FocusedInstance()
	if inst.Kind() != pane.KindDiff || inst.Diff().Editable() {
		t.Fatalf("setup: revision diff must not be editable")
	}
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m = out.(Model)
	m = chain(t, m, cmd)
	if m.panes.FocusedInstance().DiffEditor() != nil {
		t.Fatal("revision diff mounted an editor")
	}
}

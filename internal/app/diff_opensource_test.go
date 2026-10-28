package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// assertEditorAt fails unless the focused pane is an editor on path with the
// cursor on 0-based line.
func assertEditorAt(t *testing.T, m Model, path string, line int) {
	t.Helper()
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor {
		t.Fatal("Jump to Source should focus an editor")
	}
	ed := inst.Editor()
	if !ed.HasFile() || ed.Path() != canonicalPath(path) {
		t.Fatalf("Jump to Source opened %q, want %q", ed.Path(), path)
	}
	if got, _ := ed.CursorPos(); got != line {
		t.Fatalf("cursor on 0-based line %d, want %d", got, line)
	}
}

// TestDiffOpenSourceKeyOpensRightFileAtHunk guards #3081: f4 in a
// file-backed diff opens the right-hand file on the current hunk's first
// line and focuses the editor.
func TestDiffOpenSourceKeyOpensRightFileAtHunk(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(left, []byte("a\nb\nc\nd\n"), 0o644)
	os.WriteFile(right, []byte("a\nb\nC\nd\n"), 0o644)

	m := newSized()
	m.openDiffPane(left, right)
	if inst := m.activeWS().Panes.FocusedInstance(); inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatal("fixture: the diff should be focused")
	}
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyF4})
	m = drainCmd(out.(Model), cmd)
	assertEditorAt(t, m, right, 2)
}

// TestDiffOpenSourceInReviewOpensCurrentFile guards #3081 in review mode
// (#2848): the jump opens the review's current working-tree file.
func TestDiffOpenSourceInReviewOpensCurrentFile(t *testing.T) {
	m, dir, _ := reviewApp(t)
	out, cmd := m.Update(DiffFileStepMsg{Delta: 1})
	m = drainCmd(out.(Model), cmd)
	if cur, _ := reviewInstance(t, m).Diff().ReviewCurrent(); cur.Path != "b.txt" {
		t.Fatalf("fixture: review on %q, want b.txt", cur.Path)
	}
	out, cmd = m.Update(DiffOpenSourceMsg{})
	m = drainCmd(out.(Model), cmd)
	assertEditorAt(t, m, filepath.Join(dir, "b.txt"), 0)
}

// TestDiffOpenSourceWithoutFileNotifies guards #3081's no-file case: a diff
// of texts with no backing file says so and keeps the diff focused.
func TestDiffOpenSourceWithoutFileNotifies(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	m.openDiffTexts("", "left", "right", "a\n", "b\n", false)
	key := m.activeWS().Panes.Focused()
	if inst := m.activeWS().Panes.FocusedInstance(); inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatal("fixture: the diff should be focused")
	}
	out, cmd := m.Update(DiffOpenSourceMsg{})
	m = drainCmd(out.(Model), cmd)
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("focus moved to %q, want the diff %q", m.activeWS().Panes.Focused(), key)
	}
	found := false
	for _, n := range notices(m) {
		if strings.Contains(n, "no source file") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a no-source notice, got %v", notices(m))
	}
}

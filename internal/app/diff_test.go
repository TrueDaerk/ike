package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/diff"
	"ike/internal/palette"
	"ike/internal/pane"
)

// writeTempFile drops content into a temp file and returns its path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// diffKeyOf returns the key of the first diff pane, or "".
func diffKeyOf(m Model) string {
	for _, key := range m.panes.Keys() {
		if inst := m.panes.Get(key); inst != nil && inst.Kind() == pane.KindDiff {
			return key
		}
	}
	return ""
}

// TestDiffFilesPickFlow guards diff.files (#60): the command arms the
// two-step "@" pick, the two OpenFileMsg picks are intercepted instead of
// opening editors, and the second pick splits in a focused diff pane.
func TestDiffFilesPickFlow(t *testing.T) {
	m := newSized()
	left := writeTempFile(t, "left.txt", "a\nold\nc\n")
	right := writeTempFile(t, "right.txt", "a\nnew\nc\n")

	tm, _ := m.Update(DiffFilesMsg{})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("diff.files should open the file picker")
	}
	if m.diffPick != 1 {
		t.Fatalf("diffPick should be armed at 1, got %d", m.diffPick)
	}

	tm, _ = m.Update(palette.OpenFileMsg{Path: left})
	m = tm.(Model)
	if m.diffPick != 2 || m.diffLeft != left {
		t.Fatalf("first pick should arm the second: pick=%d left=%q", m.diffPick, m.diffLeft)
	}
	if !m.palette.IsOpen() {
		t.Fatal("the picker should re-open for the right file")
	}

	tm, _ = m.Update(palette.OpenFileMsg{Path: right})
	m = tm.(Model)
	if m.diffPick != 0 || m.diffLeft != "" {
		t.Fatalf("pick state should disarm after the second pick: pick=%d left=%q", m.diffPick, m.diffLeft)
	}
	key := diffKeyOf(m)
	if key == "" {
		t.Fatal("the second pick should open a diff pane")
	}
	if m.panes.Focused() != key {
		t.Fatalf("the diff pane should take focus, got %q", m.panes.Focused())
	}
	if v := m.render(); !strings.Contains(v, "DIFF") || !strings.Contains(v, "old") || !strings.Contains(v, "new") {
		t.Fatal("the rendered workspace should show the titled diff with both versions")
	}
}

// TestDiffPickDismissDisarms guards the escape hatch: dismissing the picker
// mid-flow disarms the pick state so a later "@" open is a plain file open.
func TestDiffPickDismissDisarms(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(DiffFilesMsg{})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.palette.IsOpen() {
		t.Fatal("esc should close the picker")
	}
	if m.diffPick != 0 || m.diffLeft != "" {
		t.Fatalf("dismissing the picker should disarm the pick state: pick=%d left=%q", m.diffPick, m.diffLeft)
	}
}

// TestDiffJumpOpensEditorAtLine guards enter-on-hunk: the dispatched JumpMsg
// opens the right-hand file with the cursor on the hunk line.
func TestDiffJumpOpensEditorAtLine(t *testing.T) {
	m := newSized()
	path := writeTempFile(t, "target.txt", "a\nb\nc\nd\n")
	tm, _ := m.Update(diff.JumpMsg{Path: path, Line: 3})
	m = tm.(Model)
	inst := m.panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor {
		t.Fatal("the jump should focus an editor")
	}
	ed := inst.Editor()
	if !ed.HasFile() || ed.Path() != path {
		t.Fatalf("the jump should open %q, got %q", path, ed.Path())
	}
	if line, _ := ed.CursorPos(); line != 2 {
		t.Fatalf("cursor should sit on 0-based line 2, got %d", line)
	}
}

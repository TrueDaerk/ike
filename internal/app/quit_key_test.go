package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/diff"
	"ike/internal/pane"
	"ike/internal/vcs"
)

// diffFixture opens a diff pane over two small files and returns the model
// with the diff pane focused.
func diffFixture(t *testing.T) (Model, string, string) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(left, []byte("a\n"), 0o644)
	os.WriteFile(right, []byte("b\n"), 0o644)
	m := newSized()
	m.openDiffPane(left, right)
	return m, left, right
}

// TestQKeyOnDiffPane guards #529: "q" on a focused diff pane must neither
// quit the app nor panic (quitKey used to nil-deref the missing editor).
func TestQKeyOnDiffPane(t *testing.T) {
	m, _, _ := diffFixture(t)
	out, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if isQuit(cmd) {
		t.Fatal("q on a diff pane must not quit the app")
	}
	m = out.(Model)
	if m.closePromptOpen() {
		t.Fatal("q on a diff pane must not open the quit guard")
	}
}

// TestQKeyOnVCSPane guards #529 for the VCS tool window.
func TestQKeyOnVCSPane(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	m.vcs.snap = vcs.NewSnapshot(t.TempDir(), map[string]vcs.FileStatus{})
	out, _ := m.Update(VCSPanelToggleMsg{})
	m = out.(Model)
	if inst := m.panes.FocusedInstance(); inst == nil || inst.Kind() != pane.KindVCS {
		t.Fatal("precondition: VCS pane must be focused")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if isQuit(cmd) {
		t.Fatal("q on the VCS pane must not quit the app")
	}
}

// TestDiffEditInsertTyping guards #529: with the diff pane's edit-mode editor
// (#496) capturing text, plain keys — including "q" and "?" — must reach the
// embedded editor instead of the global layer.
func TestDiffEditInsertTyping(t *testing.T) {
	m, _, right := diffFixture(t)
	key := m.panes.Focused()
	out, _ := m.Update(diff.EditRequestMsg{Key: key, Path: right})
	m = out.(Model)
	inst := m.panes.Get(key)
	if inst.DiffEditor() == nil {
		t.Fatal("precondition: diff edit mode must be active")
	}
	send := func(k tea.KeyPressMsg) {
		out, cmd := m.Update(k)
		if isQuit(cmd) {
			t.Fatalf("key %q quit the app", k.String())
		}
		m = out.(Model)
	}
	send(tea.KeyPressMsg{Code: 'i', Text: "i"})
	for _, c := range "q?" {
		send(tea.KeyPressMsg{Code: c, Text: string(c)})
	}
	if m.shell.IsOpen() {
		t.Fatal("? while typing in the diff editor must not open help")
	}
	got := m.panes.Get(key).DiffEditor().Text()
	if !strings.Contains(got, "q?") {
		t.Fatalf("typed text must reach the diff editor, got %q", got)
	}
}

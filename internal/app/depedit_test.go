package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
)

// depApp opens a file living under a dependency directory (a jumped-to
// site-packages source), so its buffer is edit-locked (#565).
func depApp(t *testing.T) (Model, string) {
	t.Helper()
	m := newSized()
	dir := filepath.Join(t.TempDir(), ".venv", "site-packages", "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mod.py")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	return tm.(Model), path
}

func TestDepEditPromptConfirmApplies(t *testing.T) {
	m, path := depApp(t)
	// First edit is blocked and stashed inside the editor.
	m = drainKey(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := m.activeEditor().Text(); got != "abc" {
		t.Fatalf("locked file was edited: %q", got)
	}
	// The editor's block signal opens the host prompt.
	tm, _ := m.Update(editor.DepEditBlockedMsg{Path: path})
	m = tm.(Model)
	if !m.depEditPromptOpen() {
		t.Fatal("dep-edit prompt did not open")
	}
	// Enter confirms: the buffer unlocks and the blocked 'x' replays.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.depEditPromptOpen() {
		t.Fatal("prompt still open after confirm")
	}
	if got := m.activeEditor().Text(); got != "bc" {
		t.Fatalf("confirm did not replay the edit: %q", got)
	}
}

func TestDepEditPromptCancelKeepsLock(t *testing.T) {
	m, path := depApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	tm, _ := m.Update(editor.DepEditBlockedMsg{Path: path})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.depEditPromptOpen() {
		t.Fatal("prompt still open after cancel")
	}
	if got := m.activeEditor().Text(); got != "abc" {
		t.Fatalf("cancel changed the file: %q", got)
	}
	if !m.activeEditor().IsDependencyFile() {
		t.Fatal("buffer should remain a locked dependency file")
	}
}

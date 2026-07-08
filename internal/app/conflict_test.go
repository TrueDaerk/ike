package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/watch"
)

// answer feeds one key to the open conflict prompt without draining follow-up
// commands (a notify's toast-expiry tick would block for seconds).
func answer(m Model, k tea.KeyPressMsg) Model {
	tm, _ := m.Update(k)
	return tm.(Model)
}

// staleApp builds the conflict precondition end-to-end: an open editor with
// unsaved edits whose file was then changed externally (watch event routed by
// the root model), so the buffer is marked stale.
func staleApp(t *testing.T) (Model, string) {
	t.Helper()
	m := newSized()
	path := filepath.Join(t.TempDir(), "c.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	if err := os.WriteFile(path, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
	return tm.(Model), path
}

func TestExternalChangeShowsStaleIndicators(t *testing.T) {
	m, _ := staleApp(t)
	ed := m.activeEditor()
	if ed == nil || !ed.Stale() {
		t.Fatal("routed watch event must mark the dirty buffer stale")
	}
	if title := m.editorTitle(ed); !strings.HasSuffix(title, "*!") {
		t.Fatalf("tab title missing the stale indicator: %q", title)
	}
	if s := m.statusLine(); !strings.Contains(s, "[disk changed]") {
		t.Fatalf("status line missing the stale segment: %q", s)
	}
}

func TestStaleSaveOpensConflictPrompt(t *testing.T) {
	m, path := staleApp(t)
	tm, _ := m.Update(editor.ConflictMsg{Path: path})
	m = tm.(Model)
	if !m.conflictOpen() {
		t.Fatal("ConflictMsg must open the prompt")
	}
	if v := m.shell.View(); !strings.Contains(v, "keep mine") || !strings.Contains(v, "reload") {
		t.Fatalf("prompt missing the choices: %q", v)
	}
}

func TestConflictKeepMineWritesAndStampsEpoch(t *testing.T) {
	m, path := staleApp(t)
	tm, _ := m.Update(editor.ConflictMsg{Path: path})
	m = tm.(Model)
	m = answer(m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Xone") {
		t.Fatalf("keep-mine must overwrite the external change: %q", data)
	}
	if ed := m.activeEditor(); ed.Stale() || ed.Dirty() {
		t.Fatal("keep-mine must clear stale and dirty")
	}
	if !m.watcher.SavedRecently(path) {
		t.Fatal("keep-mine must stamp the watcher's save epoch")
	}
	if m.conflictOpen() || m.shell.IsOpen() {
		t.Fatal("prompt must close after the answer")
	}
}

func TestConflictReloadDiscardsEdits(t *testing.T) {
	m, path := staleApp(t)
	tm, _ := m.Update(editor.ConflictMsg{Path: path})
	m = tm.(Model)
	m = answer(m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	ed := m.activeEditor()
	if got := strings.TrimRight(ed.Text(), "\n"); got != "external" {
		t.Fatalf("reload must adopt the disk content, got %q", got)
	}
	if ed.Stale() || ed.Dirty() {
		t.Fatal("reload must clear stale and dirty")
	}
	if m.conflictOpen() {
		t.Fatal("prompt must close after the answer")
	}
}

func TestConflictCancelKeepsBufferMarked(t *testing.T) {
	m, path := staleApp(t)
	tm, _ := m.Update(editor.ConflictMsg{Path: path})
	m = tm.(Model)
	m = answer(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	ed := m.activeEditor()
	if !ed.Stale() || !ed.Dirty() {
		t.Fatal("cancel must leave the buffer dirty and stale")
	}
	if m.conflictOpen() || m.shell.IsOpen() {
		t.Fatal("cancel must close the prompt")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "external\n" {
		t.Fatalf("cancel must not write: %q", data)
	}
}

func TestConflictPromptSwallowsOtherKeys(t *testing.T) {
	m, path := staleApp(t)
	tm, _ := m.Update(editor.ConflictMsg{Path: path})
	m = tm.(Model)
	m = answer(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if !m.conflictOpen() {
		t.Fatal("an unrelated key must not dismiss the prompt")
	}
	if ed := m.activeEditor(); !strings.Contains(ed.Text(), "Xone") {
		t.Fatal("keys during the prompt must not reach the editor")
	}
}

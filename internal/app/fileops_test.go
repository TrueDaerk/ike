package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
)

// drainCmd runs cmd to quiescence, expanding batches and feeding every
// produced message back into the app — the FileMovedMsg emitted by explorer
// file ops travels inside a tea.Batch, which drainKey does not unpack.
func drainCmd(m Model, cmd tea.Cmd) Model {
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		msg := c()
		if msg == nil {
			continue
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			pending = append(pending, b...)
			continue
		}
		// Skip directory-scan results: feeding them back would start the
		// explorer's auto-refresh poll loop (tea.Tick), which never quiesces.
		if _, ok := msg.(explorer.ScanDoneMsg); ok {
			continue
		}
		tm, next := m.Update(msg)
		m = tm.(Model)
		pending = append(pending, next)
	}
	return m
}

// projectDir builds a throwaway project dir with a file and a subfolder.
func projectDir(t *testing.T) (root, file string) {
	t.Helper()
	root = t.TempDir()
	file = filepath.Join(root, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, file
}

// TestRenamePromptRenamesAndEditorFollows guards the shift+f6 editor flow
// (#175): the prompt opens prefilled, enter renames on disk, and the open
// buffer follows the new path with its edits and undo history intact.
func TestRenamePromptRenamesAndEditorFollows(t *testing.T) {
	root, file := projectDir(t)
	m := newSized()
	tm, _ := m.openPath(file, false)
	m = tm.(Model)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"}, {Code: 'X', Text: "X"}, {Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyF6, Mod: tea.ModShift})
	if !m.renameOpen() {
		t.Fatal("shift+f6 with an editor focused must open the rename prompt")
	}
	if m.renameInput != "a.txt" {
		t.Fatalf("prompt must prefill the name, got %q", m.renameInput)
	}
	for range len("a.txt") {
		tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = tm.(Model)
	}
	for _, r := range "b.txt" {
		tm, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)

	renamed := filepath.Join(root, "b.txt")
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("rename did not happen on disk: %v", err)
	}
	ed := m.panes.Get(m.activeEditorKey()).Editor()
	if ed.Path() != renamed {
		t.Fatalf("editor must follow the rename, path = %q", ed.Path())
	}
	if !ed.Dirty() || !strings.HasPrefix(ed.Text(), "Xone") {
		t.Fatalf("buffer state must survive the rename: dirty=%v text=%q", ed.Dirty(), ed.Text())
	}
	// Undo history survived: revert the edit and save to the new path.
	m = drainKey(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	m = drainKey(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	data, _ := os.ReadFile(renamed)
	if !strings.HasPrefix(string(data), "one") {
		t.Fatalf("undo after rename broken; file = %q", data)
	}
}

// TestMoveRepointsOpenEditor guards the follow-on-move path: moving a file
// into a folder re-points the editor instead of closing its pane.
func TestMoveRepointsOpenEditor(t *testing.T) {
	root, file := projectDir(t)
	m := newSized()
	tm, _ := m.openPath(file, false)
	m = tm.(Model)
	key := m.activeEditorKey()

	tm, cmd := m.Update(explorer.MoveToMsg{Path: file, TargetDir: filepath.Join(root, "sub")})
	m = drainCmd(tm.(Model), cmd)

	moved := filepath.Join(root, "sub", "a.txt")
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("move did not happen on disk: %v", err)
	}
	if !m.panes.Has(key) {
		t.Fatal("the editor pane must survive the move")
	}
	if got := m.panes.Get(key).Editor().Path(); got != moved {
		t.Fatalf("editor must follow the move, path = %q", got)
	}
}

// TestDirRenameRepointsNestedEditor: renaming a folder re-points editors on
// files underneath it.
func TestDirRenameRepointsNestedEditor(t *testing.T) {
	root, _ := projectDir(t)
	nested := filepath.Join(root, "sub", "c.txt")
	if err := os.WriteFile(nested, []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(nested, false)
	m = tm.(Model)

	tm, cmd := m.Update(explorer.RenamePathMsg{Path: filepath.Join(root, "sub"), Name: "pkg"})
	m = drainCmd(tm.(Model), cmd)

	want := filepath.Join(root, "pkg", "c.txt")
	if got := m.panes.Get(m.activeEditorKey()).Editor().Path(); got != want {
		t.Fatalf("editor under a renamed folder must follow, path = %q want %q", got, want)
	}
}

// TestF6OpensDirectoryPicker guards the move entry point: f6 with an editor
// focused stashes the source and opens the palette locked to the directory
// mode.
func TestF6OpensDirectoryPicker(t *testing.T) {
	_, file := projectDir(t)
	m := newSized()
	tm, _ := m.openPath(file, false)
	m = tm.(Model)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyF6})
	if !m.palette.IsOpen() {
		t.Fatal("f6 must open the directory picker palette")
	}
	if m.movePending != file {
		t.Fatalf("movePending = %q want %q", m.movePending, file)
	}
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/pane"
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

// TestRenamePromptRenamesAndEditorFollows guards the file.rename editor flow
// (#175): the prompt opens prefilled, enter renames on disk, and the open
// buffer follows the new path with its edits and undo history intact. The
// command arrives as RenameFileMsg (palette path): with an editor focused
// shift+f6 belongs to lsp.rename now (0082 sheet 13, #18).
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

	m = dispatch(t, m, RenameFileMsg{})
	if !m.renameOpen() {
		t.Fatal("file.rename with an editor focused must open the rename prompt")
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
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
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
	if !m.activeWS().Panes.Has(key) {
		t.Fatal("the editor pane must survive the move")
	}
	if got := m.activeWS().Panes.Get(key).Editor().Path(); got != moved {
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
	if got := m.activeWS().Panes.Get(m.activeEditorKey()).Editor().Path(); got != want {
		t.Fatalf("editor under a renamed folder must follow, path = %q want %q", got, want)
	}
}

// TestPaletteFileOpFocusesExplorer guards #374: dispatching a prompt-opening
// explorer file op (the palette path) while an editor holds focus must move
// focus to the explorer so the prompt captures every typed key — previously
// the filename executed as vim commands against the buffer.
func TestPaletteFileOpFocusesExplorer(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.Msg
		// rename/delete refuse to prompt on the root selection (the fresh
		// test tree's cursor); prompt opening per selection is the explorer's
		// own tested behavior — here only the focus hand-off matters.
		wantPrompt bool
	}{
		{"newFile", explorer.NewFileMsg{}, true},
		{"newFolder", explorer.NewDirMsg{}, true},
		{"rename", explorer.RenameMsg{}, false},
		{"delete", explorer.DeleteMsg{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, file := projectDir(t)
			t.Chdir(root)
			m := newSized()
			tm, _ := m.openPath(file, false)
			m = tm.(Model)
			if m.activeWS().Panes.Focused() == pane.ExplorerKey {
				t.Fatal("setup: an editor must hold focus")
			}

			m = dispatch(t, m, tc.msg)
			if m.activeWS().Panes.Focused() != pane.ExplorerKey {
				t.Fatalf("focus = %q, want the explorer pane", m.activeWS().Panes.Focused())
			}
			if tc.wantPrompt && !m.explorer().Prompting() {
				t.Fatal("the file-op prompt must be open")
			}
		})
	}
}

// TestPaletteNewFileTypesIntoPromptNotEditor guards the end-to-end #374 repro:
// after a palette-invoked New File, typed keys land in the prompt (creating
// the file on enter) and never leak into the editor buffer as vim commands.
func TestPaletteNewFileTypesIntoPromptNotEditor(t *testing.T) {
	root, file := projectDir(t)
	t.Chdir(root)
	m := newSized()
	tm, _ := m.openPath(file, false)
	m = tm.(Model)
	editorKey := m.activeEditorKey()
	before := m.activeWS().Panes.Get(editorKey).Editor().Text()

	m = dispatch(t, m, explorer.NewFileMsg{})
	for _, r := range "util.go" {
		m = drainKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	tm2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm2.(Model), cmd)

	created := filepath.Join(root, "util.go")
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("enter must create the file on disk: %v", err)
	}
	if got := m.activeWS().Panes.Get(editorKey).Editor().Text(); got != before {
		t.Fatalf("keystrokes leaked into the editor buffer: %q -> %q", before, got)
	}
	if m.activeWS().Panes.Get(editorKey).Editor().Dirty() {
		t.Fatal("the buffer must stay clean — no leaked vim commands")
	}
}

// TestPaletteFileOpShowsHiddenExplorer: with the tree hidden (cmd+1), a
// palette file op must re-show the explorer so its prompt is visible and
// focused instead of rendering into a pane that is not in the layout.
func TestPaletteFileOpShowsHiddenExplorer(t *testing.T) {
	root, file := projectDir(t)
	t.Chdir(root)
	m := newSized()
	tm, _ := m.openPath(file, false)
	m = tm.(Model)
	m.setFocus(pane.ExplorerKey)
	m.hideExplorer()
	if m.explorerVisible() {
		t.Fatal("setup: the explorer must be hidden")
	}

	m = dispatch(t, m, explorer.NewFileMsg{})
	if !m.explorerVisible() {
		t.Fatal("a palette file op must re-show the hidden explorer")
	}
	if m.activeWS().Panes.Focused() != pane.ExplorerKey {
		t.Fatalf("focus = %q, want the explorer pane", m.activeWS().Panes.Focused())
	}
	if !m.explorer().Prompting() {
		t.Fatal("the file-op prompt must be open")
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

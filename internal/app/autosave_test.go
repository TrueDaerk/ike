package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// openDirty opens path in the app and types "iX<esc>" to dirty the buffer.
func openDirty(t *testing.T, m Model, path string) Model {
	t.Helper()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	return m
}

// TestFocusSwitchAutosavesDirtyEditor guards #174: leaving a dirty editor pane
// (tab cycles focus through setFocus, like every other navigation path) writes
// the buffer, and undo still works after returning.
func TestFocusSwitchAutosavesDirtyEditor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // focus leaves the editor
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("focus switch did not autosave; file = %q", data)
	}

	// Back to the editor: undo must still revert the autosaved edit.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = drainKey(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	m = drainKey(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	data, _ = os.ReadFile(path)
	if !strings.HasPrefix(string(data), "one") {
		t.Fatalf("undo after autosave broken; file = %q", data)
	}
}

// TestOpenReplacingDocumentAutosaves guards the second trigger: opening
// another file into the active editor saves the document it replaces.
func TestOpenReplacingDocumentAutosaves(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("one\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newSized()
	m = openDirty(t, m, a)
	tm, _ := m.openPath(b, false)
	_ = tm
	data, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("replacing the document did not autosave; file = %q", data)
	}
}

// TestAutosaveOffKeepsFileUntouched verifies editor.auto_save = "off" disables
// both triggers.
func TestAutosaveOffKeepsFileUntouched(t *testing.T) {
	if testStoreRoot == "" {
		t.Skip("no isolated config dir")
	}
	cfgDir := filepath.Join(testStoreRoot, "off-"+strconv.Itoa(int(testStoreSeq.Add(1))))
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "settings.toml"), []byte("[editor]\nauto_save = \"off\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Setenv("IKE_CONFIG_DIR", cfgDir)
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)

	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openDirty(t, m, path)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	data, _ := os.ReadFile(path)
	if string(data) != "one\n" {
		t.Fatalf("auto_save=off must not write on focus switch; file = %q", data)
	}
}

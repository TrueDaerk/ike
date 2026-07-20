package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
)

// typeInto feeds a string rune-by-rune as key presses.
func typeInto(m Model, s string) Model {
	for _, r := range s {
		m = drainKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

// dirtyUntitled types into the startup empty editor pane and returns the
// model with a dirty pathless buffer.
func dirtyUntitled(t *testing.T, m Model) Model {
	t.Helper()
	key := m.activeEditorKey()
	if key == "" {
		t.Fatal("startup layout must focus an editor")
	}
	ed := m.panes.Get(key).Editor()
	if ed == nil || ed.HasFile() {
		t.Fatal("startup editor must be an untitled buffer")
	}
	m.panes.SetFocused(key)
	m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = typeInto(m, "hello")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !ed.Dirty() {
		t.Fatal("typing must dirty the untitled buffer (#730)")
	}
	return m
}

// TestUntitledSavePromptsAndBinds guards #730: saving an untitled buffer
// prompts for a path; accepting writes the file and binds the tab to it.
func TestUntitledSavePromptsAndBinds(t *testing.T) {
	m := newSized()
	m = dirtyUntitled(t, m)
	key := m.activeEditorKey()

	tm, cmd := m.Update(editor.ActionMsg{Action: "write"})
	m = drainCmd(tm.(Model), cmd)
	if !m.saveAsOpen() {
		t.Fatal("write on an untitled buffer must open the save-as prompt")
	}

	target := filepath.Join(t.TempDir(), "sub", "new.txt")
	m = typeInto(m, target)
	tm, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)

	if m.saveAsOpen() {
		t.Fatal("accepting the prompt must close it")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("accepting the prompt must create the file: %v", err)
	}
	if !strings.HasPrefix(string(data), "hello") {
		t.Fatalf("file content = %q", data)
	}
	ed := m.panes.Get(key).Editor()
	if !ed.HasFile() || ed.Path() != target {
		t.Fatalf("tab must bind to the new file, path = %q", ed.Path())
	}
	if ed.Dirty() {
		t.Fatal("save must clear the modified indicator")
	}

	// Further saves go to the bound file without a prompt.
	m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = typeInto(m, "X")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	tm, cmd = m.Update(editor.ActionMsg{Action: "write"})
	m = drainCmd(tm.(Model), cmd)
	if m.saveAsOpen() {
		t.Fatal("a bound buffer must save without the prompt")
	}
	if data, _ := os.ReadFile(target); !strings.Contains(string(data), "X") {
		t.Fatalf("second save must hit the bound file, content = %q", data)
	}
}

// TestUntitledSaveRefusesExistingFile: the prompt never clobbers an existing
// file — it shows the error and stays open.
func TestUntitledSaveRefusesExistingFile(t *testing.T) {
	m := newSized()
	m = dirtyUntitled(t, m)

	target := filepath.Join(t.TempDir(), "exists.txt")
	if err := os.WriteFile(target, []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(editor.ActionMsg{Action: "write"})
	m = drainCmd(tm.(Model), cmd)
	m = typeInto(m, target)
	tm, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)

	if !m.saveAsOpen() {
		t.Fatal("an existing target must keep the prompt open")
	}
	if m.saveAsErr == "" {
		t.Fatal("an existing target must surface an error")
	}
	if data, _ := os.ReadFile(target); string(data) != "precious\n" {
		t.Fatalf("existing file must stay untouched, content = %q", data)
	}
	// Esc cancels; the buffer stays dirty and untitled.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.saveAsOpen() {
		t.Fatal("esc must close the prompt")
	}
	ed := m.panes.Get(m.activeEditorKey()).Editor()
	if ed.HasFile() || !ed.Dirty() {
		t.Fatal("cancel must leave the buffer untitled and dirty")
	}
}

// TestUntitledWriteQuitCarriesCloseIntent: ":wq" on an untitled buffer opens
// the prompt with the close intent armed.
func TestUntitledWriteQuitCarriesCloseIntent(t *testing.T) {
	m := newSized()
	m = dirtyUntitled(t, m)
	tm, cmd := m.Update(editor.ActionMsg{Action: "write_quit"})
	m = drainCmd(tm.(Model), cmd)
	if !m.saveAsOpen() {
		t.Fatal("write_quit on an untitled buffer must open the save-as prompt")
	}
	if !m.saveAsClose {
		t.Fatal("write_quit must carry the close-after intent into the prompt")
	}
}

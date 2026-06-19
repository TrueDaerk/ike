package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"ike/internal/editor"
	"ike/internal/explorer"
)

func newSized() Model {
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

func TestTabSwitchesFocus(t *testing.T) {
	m := newSized()
	if m.focus != focusExplorer {
		t.Fatal("should start focused on explorer")
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = tm.(Model)
	if m.focus != focusEditor {
		t.Fatal("tab should focus editor")
	}
}

func TestOpenFileRoutesToEditor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	if !m.editor.HasFile() {
		t.Fatal("editor should have loaded the file")
	}
	if m.focus != focusEditor {
		t.Fatal("opening a file should focus the editor")
	}
}

func TestCloseMsgResetsToExplorer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	tm, _ = m.Update(editor.CloseMsg{})
	m = tm.(Model)
	if m.editor.HasFile() {
		t.Fatal("close should detach the file")
	}
	if m.focus != focusExplorer {
		t.Fatal("close should focus explorer")
	}
}

func TestQuitFromExplorer(t *testing.T) {
	m := newSized()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q in explorer should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", cmd())
	}
}

// When the editor is in insert mode, a literal "q" must reach the buffer rather
// than quitting the app.
func TestQNotQuitWhileTyping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")}) // insert mode
	m = tm.(Model)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("q while typing should not quit")
		}
	}
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/search"
	"ike/internal/todoindex"
)

// todoApp opens the TODO index overlay and feeds it one streamed result
// pointing at a real file, as the wrapped scan service would. Open's rescan is
// the model's first scan, so its generation is 1.
func todoApp(t *testing.T) (Model, string) {
	t.Helper()
	m := newSized()
	path := filepath.Join(t.TempDir(), "tagged.go")
	if err := os.WriteFile(path, []byte("package x\n// TODO: fix me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(OpenTodoIndexMsg{})
	m = tm.(Model)
	if !m.todo.IsOpen() {
		t.Fatal("todo.list must open the overlay")
	}
	tm, _ = m.Update(todoindex.ScanMsg{Inner: search.BatchMsg{Gen: 1, Matches: []search.Match{
		{Path: path, Line: 2, Text: "// TODO: fix me", StartCol: 3, EndCol: 7},
	}}})
	m = tm.(Model)
	tm, _ = m.Update(todoindex.ScanMsg{Inner: search.DoneMsg{Gen: 1, Total: 1}})
	return tm.(Model), path
}

func TestTodoCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("todo.list"); !ok {
		t.Fatal("todo.list must be a registry command")
	}
}

func TestTodoOverlayRendersStreamedResults(t *testing.T) {
	m, _ := todoApp(t)
	frame := m.render()
	// The title renders styled per rune; assert on the filter row and the
	// streamed result's file name instead.
	if !strings.Contains(frame, "tagged.go") || !strings.Contains(frame, "Tag: All") {
		t.Fatal("overlay with streamed results missing from the frame")
	}
}

func TestTodoEnterOpensFileAtTag(t *testing.T) {
	m, path := todoApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.todo.IsOpen() {
		t.Fatal("enter must close the overlay")
	}
	key := m.editorKeyForPath(path)
	if key == "" {
		t.Fatal("enter must open the tagged file")
	}
	ed := m.activeWS().Panes.Get(key).Editor()
	if line, col := ed.Cursor(); line != 2 || col != 4 {
		t.Fatalf("cursor at %d,%d, want 2,4 (1-based)", line, col)
	}
}

func TestTodoStatusSegmentCountsTags(t *testing.T) {
	m, _ := todoApp(t)
	if got := m.todoSegment(); got != "1 TODO" {
		t.Fatalf("status segment = %q, want \"1 TODO\"", got)
	}
}

func TestTodoSaveRescansFile(t *testing.T) {
	m, _ := todoApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // close, keep index
	// A saved file must live under the project root (the working directory) to
	// be rescanned — the temp-dir fixture from todoApp would be skipped.
	path := "todo_rescan_fixture.txt"
	if err := os.WriteFile(path, []byte("TODO: one\nFIXME: two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	tm, cmd := m.Update(todoSavedMsg{path: path})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("an in-root save must produce a rescan command")
	}
	tm, _ = m.Update(cmd())
	m = tm.(Model)
	// The initial streamed entry plus the two rescanned tags.
	if m.todo.Count() != 3 {
		t.Fatalf("after save rescan count = %d, want 3", m.todo.Count())
	}
	// A save outside the root is skipped by design.
	if _, cmd := m.Update(todoSavedMsg{path: filepath.Join(t.TempDir(), "x.txt")}); cmd != nil {
		t.Fatal("out-of-root saves must not rescan")
	}
}

func TestTodoKeysSwallowedFromPanes(t *testing.T) {
	m, _ := todoApp(t)
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if !m.todo.IsOpen() {
		t.Fatal("plain keys must stay in the overlay")
	}
}

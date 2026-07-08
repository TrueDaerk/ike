package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
)

// deliverSync feeds the broadcast the emitter would send through the program
// loop (a goroutine in production) back into Update deterministically.
func deliverSync(m *Model, path, fromKey string) {
	tm, _ := m.Update(editor.SyncMsg{Path: path, FromKey: fromKey})
	*m = tm.(Model)
}

// sharedApp opens path in two editor panes (the second via the split-open
// flow) and returns the app plus both pane keys, focused on the second.
func sharedApp(t *testing.T) (*Model, string, [2]string) {
	t.Helper()
	m := newSized()
	path := filepath.Join(t.TempDir(), "s.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	tm, _ = m.openPath(path, true) // same file, fresh split
	m = tm.(Model)
	keys := m.editorKeysForPath(path)
	if len(keys) != 2 {
		t.Fatalf("want the file open in 2 panes, got %v", keys)
	}
	return &m, path, [2]string{keys[0], keys[1]}
}

func edOf(m *Model, key string) *editor.Model { return m.panes.Get(key).Editor() }

func TestOpenSameFileTwiceSharesDocument(t *testing.T) {
	m, _, keys := sharedApp(t)
	if !edOf(m, keys[0]).SharesBufferWith(edOf(m, keys[1])) {
		t.Fatal("two panes on one file must share the document")
	}
}

func TestEditInOnePaneMirrorsToOther(t *testing.T) {
	m, path, keys := sharedApp(t)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		tm, _ := m.Update(k) // focused pane is the second view
		*m = tm.(Model)
	}
	deliverSync(m, path, m.activeEditorKey())
	for _, key := range keys {
		ed := edOf(m, key)
		if !strings.Contains(ed.Text(), "Xone") {
			t.Fatalf("pane %s missing the unsaved edit: %q", key, ed.Text())
		}
		if !ed.Dirty() {
			t.Fatalf("pane %s must mirror the dirty flag", key)
		}
	}
}

func TestSaveInOnePaneCleansBoth(t *testing.T) {
	m, path, keys := sharedApp(t)
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		tm, _ := m.Update(k)
		*m = tm.(Model)
	}
	_ = m.panes.Get(m.activeEditorKey()).Update(editor.ActionMsg{Action: "write"})
	deliverSync(m, path, m.activeEditorKey())
	for _, key := range keys {
		if edOf(m, key).Dirty() {
			t.Fatalf("pane %s must be clean after the save in its sibling", key)
		}
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Xone") {
		t.Fatalf("save must write the shared content: %q", data)
	}
}

func TestClosingOnePaneKeepsDocumentAlive(t *testing.T) {
	m, path, _ := sharedApp(t)
	m.CloseFocused()
	keys := m.editorKeysForPath(path)
	if len(keys) != 1 {
		t.Fatalf("one pane should remain, got %v", keys)
	}
	if got := edOf(m, keys[0]).Text(); !strings.Contains(got, "one") {
		t.Fatalf("the surviving view must keep the document: %q", got)
	}
}

func TestRestoreLayoutSharesDuplicatePaths(t *testing.T) {
	// Pin the state store so the second New() restores the first one's layout
	// (newSized would rotate it away).
	t.Setenv("IKE_CONFIG_DIR", filepath.Join(t.TempDir(), "store"))
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	path := filepath.Join(t.TempDir(), "r.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.openPath(path, false)
	m = tm.(Model)
	tm, _ = m.openPath(path, true) // persists the two-pane layout
	m = tm.(Model)

	restored := New()
	keys := restored.editorKeysForPath(path)
	if len(keys) != 2 {
		t.Fatalf("restore must reopen the file in 2 panes, got %v", keys)
	}
	if !restored.panes.Get(keys[0]).Editor().SharesBufferWith(restored.panes.Get(keys[1]).Editor()) {
		t.Fatal("restored duplicate paths must share one document")
	}
}

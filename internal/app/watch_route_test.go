package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/watch"
)

// openedApp builds a sized app with path open in an editor.
func openedApp(t *testing.T, content string) (Model, string) {
	t.Helper()
	m := newSized()
	path := filepath.Join(t.TempDir(), "w.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	return tm.(Model), path
}

func TestExternalDeleteClosesCleanEditor(t *testing.T) {
	m, path := openedApp(t, "one\n")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: path})
	m = tm.(Model)
	if key := m.editorKeyForPath(path); key != "" {
		t.Fatal("an externally deleted file must not linger in a clean editor")
	}
}

func TestExternalDeleteKeepsDirtyEditorStale(t *testing.T) {
	m, path := openedApp(t, "one\n")
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: path})
	m = tm.(Model)
	key := m.editorKeyForPath(path)
	if key == "" {
		t.Fatal("a dirty editor must survive an external delete — its buffer is the only copy")
	}
	if ed := m.activeWS().Panes.Get(key).Editor(); !ed.Stale() || !ed.Dirty() {
		t.Fatal("the surviving buffer must be dirty and stale")
	}
}

func TestRemoveWithFilePresentIsAContentChange(t *testing.T) {
	// A replace-in-place save (write temp + rename, git checkout) coalesces to
	// FileRemoved although the path exists again: it must reload, not close.
	m, path := openedApp(t, "one\n")
	if err := os.WriteFile(path, []byte("replaced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileRemoved, Path: path})
	m = tm.(Model)
	key := m.editorKeyForPath(path)
	if key == "" {
		t.Fatal("the editor must stay open for a replaced file")
	}
	if got := m.activeWS().Panes.Get(key).Editor().Text(); !strings.Contains(got, "replaced") {
		t.Fatalf("a replaced file must reload like a content change, got %q", got)
	}
}

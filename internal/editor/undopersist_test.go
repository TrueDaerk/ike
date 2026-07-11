package editor

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/host"
)

// TestMain pins IKE_CONFIG_DIR to a throwaway directory so editor tests that
// save (and thereby persist undo, #148) never write a ".ike" state store into
// the package directory. Persistent-undo tests below override it per test.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ike-editor-test-")
	if err == nil {
		os.Setenv("IKE_CONFIG_DIR", dir)
	}
	code := m.Run()
	if err == nil {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

// editSaved loads path, appends text via insert mode, and writes the buffer —
// leaving a persisted undo file behind.
func editSaved(t *testing.T, path, text string) {
	t.Helper()
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m = send(m, key('A'))
	m = typeKeys(m, text)
	m = send(m, special(27)) // esc commits the insert as one change
	m, _ = m.Update(ActionMsg{Action: "write"})
	if m.Dirty() {
		t.Fatal("write action left the buffer dirty")
	}
}

func TestPersistentUndoSurvivesReload(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	editSaved(t, path, "!")

	// A fresh editor (restart) reloads the file unchanged: the history is
	// adopted and `u` reverts the previous session's edit.
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	if m.Dirty() {
		t.Fatal("freshly loaded buffer must be clean")
	}
	m = send(m, key('u'))
	if got := m.Text(); got != "one" {
		t.Errorf("after restored undo: %q, want %q", got, "one")
	}
	if !m.Dirty() {
		t.Error("undo away from the saved state must mark the buffer dirty")
	}
}

func TestPersistentUndoDiscardsOnExternalChange(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	editSaved(t, path, "!")
	// The file changes outside IKE between sessions (git checkout): the stored
	// stacks no longer describe this content and must be discarded.
	if err := os.WriteFile(path, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m = send(m, key('u'))
	if got := m.Text(); got != "tampered" {
		t.Errorf("undo after external change mutated the buffer: %q", got)
	}
}

func TestPersistentUndoConfigOff(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	editSaved(t, path, "!")

	m := New()
	m.Configure(host.MapConfig{"files.persistent_undo": "false"})
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m = send(m, key('u'))
	if got := m.Text(); got != "one!" {
		t.Errorf("undo with persistence off mutated the buffer: %q", got)
	}
}

func TestPersistentUndoSharedDocument(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	editSaved(t, path, "!")

	// Session restore of a shared document (#142): the first view Loads (and
	// adopts the history once); the second aliases it and can undo too.
	first := New()
	if err := first.Load(path); err != nil {
		t.Fatal(err)
	}
	first.SetSize(80, 20)
	second := New()
	second.ShareDocumentWith(&first)
	second.SetSize(80, 20)
	second.SetFocused(true)
	second = send(second, key('u'))
	if got := first.Text(); got != "one" {
		t.Errorf("undo in the second view must hit the shared document: %q", got)
	}
}

func TestPersistUndoSkipsDirtyBuffer(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m = send(m, key('A'))
	m = typeKeys(m, "!")
	m = send(m, special(27))
	if !m.Dirty() {
		t.Fatal("edit should dirty the buffer")
	}
	m.PersistUndo() // dirty: the stacks describe text not on disk — no write

	fresh := New()
	if err := fresh.Load(path); err != nil {
		t.Fatal(err)
	}
	fresh.SetSize(80, 20)
	fresh = send(fresh, key('u'))
	if got := fresh.Text(); got != "one" {
		t.Errorf("dirty PersistUndo leaked stacks: %q", got)
	}
}

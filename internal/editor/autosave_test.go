package editor

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestAutosaveWritesDirtyBufferAndKeepsHistory guards the core auto-save
// contract (#174): a dirty buffer is written through the normal save path,
// the dirty flag clears, and undo history survives — undoing after an
// auto-save still reverts the edit.
func TestAutosaveWritesDirtyBufferAndKeepsHistory(t *testing.T) {
	m, path := loaded(t, "one\ntwo\n")
	m = send(m, key('i'), key('X'), special(tea.KeyEscape))
	if !m.Dirty() {
		t.Fatal("edit must dirty the buffer")
	}
	if !m.Autosave() {
		t.Fatal("Autosave must write a dirty buffer")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("autosave wrote %q, want it to start with Xone", data)
	}
	if m.Dirty() {
		t.Fatal("autosave must clear the dirty flag")
	}
	m = send(m, key('u'))
	if !strings.HasPrefix(m.buf.String(), "one") {
		t.Fatalf("undo after autosave must revert the edit, buffer = %q", m.buf.String())
	}
	if !m.Dirty() {
		t.Fatal("undoing past the saved state must re-dirty the buffer")
	}
}

// TestAutosaveSkipsCleanBuffer verifies a clean buffer is never rewritten.
func TestAutosaveSkipsCleanBuffer(t *testing.T) {
	m, path := loaded(t, "one\n")
	if m.Autosave() {
		t.Fatal("Autosave must be a no-op on a clean buffer")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "one\n" {
		t.Fatalf("clean buffer autosave must not touch the file, got %q", data)
	}
}

// TestAutosaveSkipsStaleBuffer guards the conflict rule: auto-save must never
// clobber an external change; the buffer stays dirty for the explicit-save
// conflict prompt.
func TestAutosaveSkipsStaleBuffer(t *testing.T) {
	m, path := staleEditor(t)
	if m.Autosave() {
		t.Fatal("Autosave must skip a stale buffer")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "external\n" {
		t.Fatalf("autosave clobbered the external change: %q", data)
	}
	if !m.Dirty() {
		t.Fatal("a skipped stale buffer must stay dirty")
	}
}

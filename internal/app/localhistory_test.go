package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// TestLocalHistoryRecordsOnSave: the save-side hook (#1023) stores the
// written bytes, and an identical second save dedupes to one entry.
func TestLocalHistoryRecordsOnSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)
	m = drainKey(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}) // save

	// The emitter forwards EventSave from a goroutine; deliver its message
	// deterministically here.
	tm, _ := m.Update(localHistorySnapshotMsg{path: path})
	m = tm.(Model)
	entries := m.lhStore.List(path)
	if len(entries) != 1 {
		t.Fatalf("List = %d entries after save, want 1", len(entries))
	}
	data, err := m.lhStore.Read(entries[0].Hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("snapshot = %q, want the saved content (Xone…)", data)
	}

	// A second save without changes stores nothing new.
	tm, _ = m.Update(localHistorySnapshotMsg{path: path})
	m = tm.(Model)
	if n := len(m.lhStore.List(path)); n != 1 {
		t.Fatalf("List = %d entries after identical save, want 1 (dedupe)", n)
	}
}

// TestLocalHistoryRestoreThroughBuffer: restoring a snapshot rewrites the
// buffer through the edit path — marks it dirty, leaves the file on disk
// untouched, and a single undo reverts it.
func TestLocalHistoryRestoreThroughBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path) // buffer: "Xone\ntwo\n", dirty
	m.lhStore.Record(path, []byte("SNAP\n"))

	m.openLocalHistoryPicker()
	if !m.localHistoryOpen() {
		t.Fatal("picker did not open")
	}
	tm, _ := m.updateLocalHistoryPicker(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = tm.(Model)

	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("no active editor")
	}
	if got := ed.Text(); got != "SNAP" {
		t.Fatalf("buffer after restore = %q, want %q (buffer form, no final newline)", got, "SNAP")
	}
	if !ed.Dirty() {
		t.Fatal("restore did not mark the buffer dirty")
	}
	if data, _ := os.ReadFile(path); string(data) != "one\ntwo\n" {
		t.Fatalf("restore touched the file on disk: %q", data)
	}

	// One undo brings the pre-restore content back.
	m = drainKey(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := m.activeEditor().Text(); got != "Xone\ntwo" {
		t.Fatalf("buffer after undo = %q, want %q", got, "Xone\ntwo")
	}
}

// TestLocalHistoryEnterOpensDiffPane: enter on a snapshot opens the reusable
// diff pane with the snapshot on the left and the live buffer on the right.
func TestLocalHistoryEnterOpensDiffPane(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)
	m.lhStore.Record(path, []byte("SNAP\n"))

	m.openLocalHistoryPicker()
	if !m.localHistoryOpen() {
		t.Fatal("picker did not open")
	}
	tm, _ := m.updateLocalHistoryPicker(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.localHistoryOpen() {
		t.Fatal("picker still open after enter")
	}
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatalf("focused pane after enter = %q, want a diff pane", key)
	}
	if inst.Diff().HunkCount() == 0 {
		t.Fatal("diff pane shows no hunks for differing contents")
	}
}

// TestLocalHistoryPickerNeedsSnapshots: without history the command degrades
// to a notice instead of an empty modal.
func TestLocalHistoryPickerNeedsSnapshots(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	m.openLocalHistoryPicker()
	if m.localHistoryOpen() {
		t.Fatal("picker opened with no snapshots")
	}
}

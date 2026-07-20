package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
)

// newWithSettings builds a sized app over an isolated config dir holding the
// given settings.toml content.
func newWithSettings(t *testing.T, toml string) Model {
	t.Helper()
	if testStoreRoot == "" {
		t.Skip("no isolated config dir")
	}
	cfgDir := filepath.Join(testStoreRoot, "idle-"+strconv.Itoa(int(testStoreSeq.Add(1))))
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "settings.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Setenv("IKE_CONFIG_DIR", cfgDir)
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

// TestIdleAutosaveMarksAndSaves guards #731: with auto_save = "idle", the
// change seam arms the idle debounce and an expired deadline writes the
// buffer through the normal save path, clearing the modified indicator.
func TestIdleAutosaveMarksAndSaves(t *testing.T) {
	m := newWithSettings(t, "[editor]\nauto_save = \"idle\"\nauto_save_idle_ms = 500\n")
	if m.autosaveIdleIv != 500*time.Millisecond {
		t.Fatalf("idle interval = %v, want 500ms", m.autosaveIdleIv)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openDirty(t, m, path)
	key := m.activeEditorKey()

	tm, _ := m.Update(editor.SyncMsg{Path: path, FromKey: key})
	m = tm.(Model)
	if m.autosaveIdleDeb.Pending() != 1 {
		t.Fatalf("change on a dirty buffer must arm the idle debounce, pending = %d", m.autosaveIdleDeb.Pending())
	}

	// Before the deadline nothing writes.
	m.saveDueIdleBuffers(time.Now())
	if data, _ := os.ReadFile(path); string(data) != "one\n" {
		t.Fatalf("idle save must wait for the deadline; file = %q", data)
	}

	// Past the deadline the buffer saves and goes clean.
	m.saveDueIdleBuffers(time.Now().Add(time.Minute))
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("expired idle deadline must write the buffer; file = %q", data)
	}
	if m.panes.Get(key).Editor().Dirty() {
		t.Fatal("idle save must clear the modified indicator")
	}
}

// TestIdleAutosaveSkipsUntitled: a pathless buffer never arms the idle
// debounce — autosave has nowhere to write it.
func TestIdleAutosaveSkipsUntitled(t *testing.T) {
	m := newWithSettings(t, "[editor]\nauto_save = \"idle\"\n")
	key := m.panes.Focused()
	tm, _ := m.Update(editor.SyncMsg{Path: "", FromKey: key})
	m = tm.(Model)
	if m.autosaveIdleDeb.Pending() != 0 {
		t.Fatalf("untitled buffer must not arm the idle debounce, pending = %d", m.autosaveIdleDeb.Pending())
	}
}

// TestIdleAutosaveOffInFocusMode: the default "focus" mode never arms the
// idle debounce (and "idle" keeps the on-blur save from #174 active, which
// TestFocusSwitchAutosavesDirtyEditor covers for the shared path).
func TestIdleAutosaveOffInFocusMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	m = openDirty(t, m, path)
	key := m.activeEditorKey()
	tm, _ := m.Update(editor.SyncMsg{Path: path, FromKey: key})
	m = tm.(Model)
	if m.autosaveIdleDeb.Pending() != 0 {
		t.Fatalf("focus mode must not arm the idle debounce, pending = %d", m.autosaveIdleDeb.Pending())
	}
	m.saveDueIdleBuffers(time.Now().Add(time.Minute))
	if data, _ := os.ReadFile(path); string(data) != "one\n" {
		t.Fatalf("focus mode must never idle-write; file = %q", data)
	}
}

// TestIdleAutosaveSaveCancelsMark: a save (clean buffer on the seam) drops
// the pending idle mark.
func TestIdleAutosaveSaveCancelsMark(t *testing.T) {
	m := newWithSettings(t, "[editor]\nauto_save = \"idle\"\n")
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openDirty(t, m, path)
	key := m.activeEditorKey()
	tm, _ := m.Update(editor.SyncMsg{Path: path, FromKey: key})
	m = tm.(Model)
	if m.autosaveIdleDeb.Pending() != 1 {
		t.Fatal("dirty change must arm")
	}
	tm, _ = m.Update(editor.ActionMsg{Action: "write"})
	m = tm.(Model)
	tm, _ = m.Update(editor.SyncMsg{Path: path, FromKey: key})
	m = tm.(Model)
	if m.autosaveIdleDeb.Pending() != 0 {
		t.Fatalf("clean buffer on the seam must cancel the mark, pending = %d", m.autosaveIdleDeb.Pending())
	}
}

package app

import (
	"testing"

	"ike/internal/backup"
	"ike/internal/terminal"
)

// snapshot_nil_test.go guards #931: an editor-KIND pane whose active tab is a
// terminal (#573, #836) has Instance.Editor() == nil; snapshotSession (project
// switch, quit) and its callers must skip the editor part instead of panicking.

// termTabActive turns the active editor pane's focus onto a terminal tab (a
// failed-spawn terminal model keeps the test process-free) and returns its key.
func termTabActive(t *testing.T, m Model) string {
	t.Helper()
	key := m.activeEditorKey()
	if key == "" {
		t.Fatal("setup: no editor pane")
	}
	inst := m.activeWS().Panes.Get(key)
	tm := terminal.New("terminal-test", "/nonexistent-shell-for-test", ".", 10, 4, nil, nil)
	if inst.AddTerminalTab(tm) == nil {
		t.Fatal("setup: AddTerminalTab failed")
	}
	if inst.Editor() != nil {
		t.Fatal("setup: active tab should be the terminal (Editor() nil)")
	}
	m.setFocus(key)
	return key
}

func TestSnapshotSessionWithActiveTerminalTab(t *testing.T) {
	m := sized(t, 100, 40)
	termTabActive(t, m)
	// The crash path (#931): activeEditorKey returns the pane, Editor() is nil.
	s := m.snapshotSession() // must not panic
	if s.Editor != nil {
		t.Fatalf("no editor snapshot expected, got %+v", s.Editor)
	}
}

func TestQuitWithActiveTerminalTab(t *testing.T) {
	m := sized(t, 100, 40)
	termTabActive(t, m)
	// quit runs snapshotSession on the same path (#931).
	if _, cmd := m.quit(); cmd == nil {
		t.Fatal("quit must still return the exit command")
	}
}

func TestEditorNormalModeWithActiveTerminalTab(t *testing.T) {
	m := sized(t, 100, 40)
	termTabActive(t, m)
	if m.editorNormalMode() {
		t.Fatal("a terminal tab has no editor normal mode")
	}
}

func TestRestoreSnapshotWithActiveTerminalTab(t *testing.T) {
	m := sized(t, 100, 40)
	termTabActive(t, m)
	// Recovery restore (#931 audit): must land the text in a fresh editor tab
	// instead of dereferencing the nil editor.
	m.restoreSnapshot(backup.Snapshot{Path: "recovered.txt", Text: "recovered text"})
	key := m.activeEditorKey()
	ed := m.activeWS().Panes.Get(key).Editor()
	if ed == nil {
		t.Fatal("restore must create an editor tab")
	}
}

func TestActiveEditorKeyStillNamesTerminalTabPane(t *testing.T) {
	// Documented invariant (#931): the key names an editor-kind pane even
	// when its active tab is a terminal — callers nil-check Editor().
	m := sized(t, 100, 40)
	key := termTabActive(t, m)
	if got := m.activeEditorKey(); got != key {
		t.Fatalf("activeEditorKey = %q, want %q", got, key)
	}
}

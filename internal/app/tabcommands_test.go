package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
)

// tabcommands_test.go covers the editor.tab.* commands (#158): cycling,
// selecting, reordering, the closed-tab reopen ring, and keymap delivery.

// tabApp opens three files as tabs in one pane and returns the model plus
// their paths. The third tab is active.
func tabApp(t *testing.T) (Model, [3]string) {
	t.Helper()
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	c := writeTemp(t, dir, "c.txt", "ccc\n")
	m := openApp(t, a, b, c)
	if m.panes.FocusedInstance().TabCount() != 3 {
		t.Fatal("setup: want 3 tabs")
	}
	return m, [3]string{a, b, c}
}

// dispatch feeds a command message through Update.
func dispatch(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	tm, _ := m.Update(msg)
	return tm.(Model)
}

func TestTabCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{
		"editor.tab.next", "editor.tab.prev",
		"editor.tab.moveLeft", "editor.tab.moveRight",
		"editor.tab.reopenClosed",
		"editor.tab.select1", "editor.tab.select9",
	} {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("command %s must be registered", id)
		}
	}
}

func TestTabNextPrevWrap(t *testing.T) {
	m, paths := tabApp(t)
	inst := m.panes.FocusedInstance()

	m = dispatch(t, m, TabStepMsg{Delta: 1}) // active idx 2 → wraps to 0
	if inst.Editor().Path() != paths[0] {
		t.Fatalf("next from the last tab must wrap to the first, got %q", inst.Editor().Path())
	}
	m = dispatch(t, m, TabStepMsg{Delta: -1}) // back to idx 2
	if inst.Editor().Path() != paths[2] {
		t.Fatalf("prev from the first tab must wrap to the last, got %q", inst.Editor().Path())
	}
}

func TestTabSelect(t *testing.T) {
	m, paths := tabApp(t)
	inst := m.panes.FocusedInstance()

	m = dispatch(t, m, TabSelectMsg{Index: 1})
	if inst.Editor().Path() != paths[1] || inst.ActiveTab() != 1 {
		t.Fatalf("select2 must activate the second tab, got %q", inst.Editor().Path())
	}
	m = dispatch(t, m, TabSelectMsg{Index: 7}) // out of range: no-op
	if inst.ActiveTab() != 1 {
		t.Fatal("out-of-range select must be a no-op")
	}
}

func TestTabMoveReorders(t *testing.T) {
	m, paths := tabApp(t)
	inst := m.panes.FocusedInstance()

	m = dispatch(t, m, TabMoveMsg{Delta: -1}) // c moves from idx 2 to idx 1
	if inst.Editor().Path() != paths[2] || inst.ActiveTab() != 1 {
		t.Fatalf("the moved tab must stay active at its new position, active=%d %q",
			inst.ActiveTab(), inst.Editor().Path())
	}
	if inst.TabEditor(2).Path() != paths[1] {
		t.Fatal("the displaced tab must slide right")
	}
	m = dispatch(t, m, TabMoveMsg{Delta: 5}) // past the end: no-op
	if inst.ActiveTab() != 1 {
		t.Fatal("a move past the end must be a no-op")
	}
}

func TestReopenClosedTabRestoresPathAndCursor(t *testing.T) {
	m, paths := tabApp(t)
	inst := m.panes.FocusedInstance()

	// Park the caret somewhere recognisable in c.txt, then close its tab.
	inst.Editor().SetCursor(0, 2)
	m.CloseFocused()
	if inst.TabCount() != 2 {
		t.Fatal("setup: close must peel the active tab")
	}

	m = dispatch(t, m, TabReopenMsg{})
	inst = m.panes.FocusedInstance()
	if inst.TabCount() != 3 || inst.Editor().Path() != paths[2] {
		t.Fatalf("reopen must restore the closed file, got %q", inst.Editor().Path())
	}
	if line, col := inst.Editor().CursorPos(); line != 0 || col != 2 {
		t.Fatalf("reopen must restore the caret, got %d,%d", line, col)
	}
}

func TestReopenEmptyRingNotifies(t *testing.T) {
	m, _ := tabApp(t)
	m = dispatch(t, m, TabReopenMsg{}) // nothing closed yet
	found := false
	for _, tst := range m.toasts {
		if strings.Contains(tst.text, "no closed tabs") {
			found = true
		}
	}
	if !found {
		t.Fatal("an empty reopen ring must surface a notification")
	}
}

func TestPaneCloseFeedsReopenRing(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "aaa\n")
	b := writeTemp(t, dir, "b.txt", "bbb\n")
	m := openApp(t, a)
	tm, _ := m.openPath(b, true) // b in a second pane
	m = tm.(Model)
	m.CloseFocused() // single-tab pane: closes the pane itself

	m = dispatch(t, m, TabReopenMsg{})
	inst := m.panes.FocusedInstance()
	if inst.EditorForPath(b) == nil {
		t.Fatal("a tab lost with its pane must be reopenable")
	}
}

func TestTabKeymapChords(t *testing.T) {
	m, paths := tabApp(t)
	inst := m.panes.FocusedInstance()

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl | tea.ModAlt})
	if inst.Editor().Path() != paths[0] {
		t.Fatalf("ctrl+alt+right must cycle to the next tab, got %q", inst.Editor().Path())
	}
	m = drainKey(m, tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
	if inst.Editor().Path() != paths[1] {
		t.Fatalf("alt+2 must jump to the second tab, got %q", inst.Editor().Path())
	}
}

// The alt+arrow tab secondaries were freed for word-wise cursor motion (#303):
// the chord must fall through the keymap to the editor instead of cycling tabs.
func TestAltArrowReachesEditorWordMotion(t *testing.T) {
	m, paths := tabApp(t)
	inst := m.panes.FocusedInstance()

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt})
	if inst.Editor().Path() != paths[2] {
		t.Fatalf("alt+right must not switch tabs anymore, got %q", inst.Editor().Path())
	}
	// "ccc" is the only word on the line: the in-line motion clamps to the
	// line end, which normal mode snaps onto the last rune.
	if _, col := inst.Editor().CursorPos(); col != 2 {
		t.Fatalf("alt+right must move the cursor word-wise, col=%d want 2", col)
	}
}

func TestTabDefaultsConflictFree(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		tbl := keymap.BuildTable(keymap.Defaults(keymap.PresetJetBrains), nil, goos)
		for _, c := range tbl.Conflicts() {
			t.Fatalf("default set must stay conflict-free on %s, found %v", goos, c)
		}
	}
}

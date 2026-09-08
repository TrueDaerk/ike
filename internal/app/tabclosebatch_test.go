package app

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
	"ike/internal/layout"
	"ike/internal/menu"
)

// tabclosebatch_test.go covers the batch tab closes of #2538:
// editor.tab.closeLeft / closeRight / closeUnmodified / closeAll next to the
// existing closeOthers — their pinned-tab exemption, their shared
// unsaved-changes guard, and the surfaces that offer them.

// tabApp5 opens five files as tabs in one pane and returns the model plus
// their paths, with the third tab (index 2) active so both sides are
// populated.
func tabApp5(t *testing.T) (Model, [5]string) {
	t.Helper()
	dir := t.TempDir()
	var paths [5]string
	for i, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		paths[i] = writeTemp(t, dir, name, name+"\n")
	}
	m := openApp(t, paths[0], paths[1], paths[2], paths[3], paths[4])
	inst := m.activeWS().Panes.FocusedInstance()
	if inst.TabCount() != 5 {
		t.Fatalf("setup: want 5 tabs, got %d", inst.TabCount())
	}
	m = dispatch(t, m, TabSelectMsg{Index: 2})
	if inst.ActiveTab() != 2 {
		t.Fatalf("setup: want tab 2 active, got %d", inst.ActiveTab())
	}
	return m, paths
}

// answerCloseGuard feeds one key into the open unsaved-changes guard and runs
// the commands it produces. It calls the guard's own update rather than
// pressing the key into the model: the KeyPressMsg chain that reaches the
// guard sits behind the onboarding overlay, whose state a headless model test
// does not control.
func answerCloseGuard(t *testing.T, m Model, k tea.KeyPressMsg) Model {
	t.Helper()
	if !m.closePromptOpen() {
		t.Fatal("no unsaved-changes guard is open to answer")
	}
	out, cmd := m.updateClosePrompt(k)
	return drainCmd(out.(Model), cmd)
}

func TestBatchCloseCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{
		"editor.tab.closeOthers", "editor.tab.closeLeft", "editor.tab.closeRight",
		"editor.tab.closeUnmodified", "editor.tab.closeAll",
	} {
		if _, ok := m.reg.Command(id); !ok {
			t.Errorf("%s must be a registry command", id)
		}
	}
}

// TestCloseTabsLeftRight guards #2538: the side closes drop exactly the tabs
// before / after the active one and leave it active.
func TestCloseTabsLeftRight(t *testing.T) {
	m, paths := tabApp5(t)
	inst := m.activeWS().Panes.FocusedInstance()
	m = dispatch(t, m, TabCloseSideMsg{Delta: -1})
	if inst.TabCount() != 3 {
		t.Fatalf("closeLeft must drop the two tabs before the active one, got %d", inst.TabCount())
	}
	if inst.TabForPath(paths[0]) >= 0 || inst.TabForPath(paths[1]) >= 0 {
		t.Fatal("closeLeft must close the tabs to the left")
	}
	if inst.Editor().Path() != paths[2] {
		t.Fatalf("the active tab must survive closeLeft, got %q", inst.Editor().Path())
	}
	m = dispatch(t, m, TabCloseSideMsg{Delta: 1})
	if inst.TabCount() != 1 || inst.Editor().Path() != paths[2] {
		t.Fatalf("closeRight must leave only the active tab, got %d", inst.TabCount())
	}
}

// TestCloseTabsSideIgnoresPinned guards #2538 against #1172: a pinned tab on
// the closing side stays open and is reported.
func TestCloseTabsSideIgnoresPinned(t *testing.T) {
	m, paths := tabApp5(t)
	inst := m.activeWS().Panes.FocusedInstance()
	m = dispatch(t, m, TabSelectMsg{Index: 0})
	m = dispatch(t, m, TabTogglePinMsg{}) // pin a.txt
	m = dispatch(t, m, TabSelectMsg{Index: 2})
	m = dispatch(t, m, TabCloseSideMsg{Delta: -1})
	if inst.TabForPath(paths[0]) < 0 {
		t.Fatal("a pinned tab must survive Close Tabs to the Left")
	}
	if inst.TabForPath(paths[1]) >= 0 {
		t.Fatal("the unpinned tab to the left must still close")
	}
}

// TestCloseUnmodifiedTabs guards #2538: every clean tab goes — the active one
// included — and only the edited buffers stay.
func TestCloseUnmodifiedTabs(t *testing.T) {
	m, paths := tabApp5(t)
	inst := m.activeWS().Panes.FocusedInstance()
	inst.TabEditor(1).RestoreText("edited\n")
	inst.TabEditor(3).RestoreText("edited\n")
	m = dispatch(t, m, TabCloseUnmodifiedMsg{})
	if inst.TabCount() != 2 {
		t.Fatalf("only the two dirty tabs may survive, got %d", inst.TabCount())
	}
	if inst.TabForPath(paths[1]) < 0 || inst.TabForPath(paths[3]) < 0 {
		t.Fatal("the dirty tabs must stay open")
	}
	if inst.TabForPath(paths[2]) >= 0 {
		t.Fatal("the clean active tab must close with the rest")
	}
	// Nothing was dirty in the batch, so no guard was needed.
	if m.closePending != nil {
		t.Fatal("closeUnmodified must never open the unsaved-changes guard")
	}
}

// TestCloseAllTabsClosesThePane guards #2538: with nothing left to show, the
// pane closes with its last tab — the way cmd+w on a single-tab pane behaves.
func TestCloseAllTabsClosesThePane(t *testing.T) {
	m, _ := tabApp5(t)
	key := m.activeWS().Panes.Focused()
	// A second editor pane, so the tab pane is not the workspace's last leaf.
	m = dispatch(t, m, SplitFocusedMsg{Zone: layout.ZoneRight})
	m.setFocus(key)
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.TabCount() != 5 {
		t.Fatalf("setup: the split must leave the five-tab pane intact, got %v", inst)
	}
	m = dispatch(t, m, TabCloseAllMsg{})
	if m.activeWS().Panes.Has(key) {
		t.Fatal("closeAll must take the pane with its last tab")
	}
}

// TestCloseAllTabsKeepsPinned guards #2538 against #1172: a pinned tab holds
// the pane open, and Close All takes everything else.
func TestCloseAllTabsKeepsPinned(t *testing.T) {
	m, paths := tabApp5(t)
	inst := m.activeWS().Panes.FocusedInstance()
	key := m.activeWS().Panes.Focused()
	m = dispatch(t, m, TabSelectMsg{Index: 3})
	m = dispatch(t, m, TabTogglePinMsg{}) // pin d.txt
	m = dispatch(t, m, TabCloseAllMsg{})
	if !m.activeWS().Panes.Has(key) {
		t.Fatal("a pinned tab must hold the pane open through Close All")
	}
	if inst.TabCount() != 1 || inst.TabForPath(paths[3]) < 0 {
		t.Fatalf("only the pinned tab may survive Close All, got %d tabs", inst.TabCount())
	}
}

// TestBatchCloseSavesEveryDirtyTabOnce guards #2538: the guard answers for the
// whole batch — "s" writes every dirty victim and then closes them all,
// instead of prompting once per file.
func TestBatchCloseSavesEveryDirtyTabOnce(t *testing.T) {
	m, paths := tabApp5(t)
	inst := m.activeWS().Panes.FocusedInstance()
	inst.TabEditor(0).RestoreText("left edit\n")
	inst.TabEditor(1).RestoreText("left edit too\n")
	m = dispatch(t, m, TabCloseSideMsg{Delta: -1})
	if m.closePending == nil || len(m.closePending.dirty) != 2 {
		t.Fatalf("both dirty tabs must be named in one prompt, got %+v", m.closePending)
	}
	m = answerCloseGuard(t, m, tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.closePending != nil {
		t.Fatal("a successful save must settle the batch, not re-prompt")
	}
	if inst.TabCount() != 3 {
		t.Fatalf("saving must then close both tabs, got %d", inst.TabCount())
	}
	if inst.TabForPath(paths[0]) >= 0 || inst.TabForPath(paths[1]) >= 0 {
		t.Fatal("the saved tabs must close")
	}
	if data, _ := os.ReadFile(paths[0]); string(data) != "left edit\n" {
		t.Fatalf("tab 0 must have been written, file holds %q", string(data))
	}
}

// TestBatchCloseSurfaces guards #2538's discoverability: the tab context menu
// and the File menu both offer every batch close.
func TestBatchCloseSurfaces(t *testing.T) {
	want := []string{
		"editor.tab.closeOthers", "editor.tab.closeLeft", "editor.tab.closeRight",
		"editor.tab.closeUnmodified", "editor.tab.closeAll",
	}
	has := func(items []menu.Item, id string) bool {
		for _, it := range items {
			if it.Command == id {
				return true
			}
		}
		return false
	}
	for _, id := range want {
		if !has(tabContextItems(false), id) {
			t.Errorf("the tab context menu must offer %s", id)
		}
	}
	var file []menu.Item
	for _, mn := range menu.Defaults() {
		if mn.Title == "File" {
			file = mn.Items
		}
	}
	for _, id := range want {
		if !has(file, id) {
			t.Errorf("the File menu must offer %s", id)
		}
	}
}

// TestCloseOthersDefaultChord guards #2538's keybind: cmd+alt+w reaches
// editor.tab.closeOthers on macOS, where the Cmd modifier is delivered.
func TestCloseOthersDefaultChord(t *testing.T) {
	found := false
	for _, b := range keymap.DefaultsFor(keymap.PresetJetBrains, "darwin") {
		if b.Command == "editor.tab.closeOthers" && b.Chord.String() == "cmd+alt+w" {
			found = true
		}
	}
	if !found {
		t.Fatal("editor.tab.closeOthers must ship cmd+alt+w on macOS (#2538)")
	}
	// Off macOS the Cmd→Ctrl fold would collide with pane.close's ctrl+alt+w,
	// so the chord stays darwin-only and the menus are the doorway there.
	for _, b := range keymap.DefaultsFor(keymap.PresetJetBrains, "linux") {
		if b.Command == "editor.tab.closeOthers" {
			t.Fatal("the cmd+alt+w chord must not ship off macOS (ctrl+alt+w is pane.close)")
		}
	}
}

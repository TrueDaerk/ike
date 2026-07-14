package pane

import (
	"testing"

	"ike/internal/terminal"
)

// termTab appends a terminal tab hosting a (failed-spawn) terminal model —
// the session never starts, which keeps the test process-free while the tab
// slot behaves like any terminal tab (#573).
func termTab(t *testing.T, i *Instance) *terminal.Model {
	t.Helper()
	tm := terminal.New("terminal-test", "/nonexistent-shell-for-test", ".", 10, 4, nil, nil)
	term := i.AddTerminalTab(tm)
	if term == nil {
		t.Fatal("AddTerminalTab returned nil on an editor instance")
	}
	return term
}

// TestAddTerminalTabActivates verifies a terminal tab appends at the end,
// becomes active, and flips the instance into terminal-flavoured accessors.
func TestAddTerminalTabActivates(t *testing.T) {
	i := editorInst(t)
	loadTab(t, i, "a.txt")
	term := termTab(t, i)
	if i.TabCount() != 2 || i.ActiveTab() != 1 {
		t.Fatalf("after AddTerminalTab: tabs=%d active=%d, want 2/1", i.TabCount(), i.ActiveTab())
	}
	if i.Editor() != nil {
		t.Fatal("Editor() must be nil while a terminal tab is active")
	}
	if i.ActiveTerminal() != term {
		t.Fatal("ActiveTerminal() must return the active tab's terminal")
	}
	if i.ContextID() != ctxTerminal {
		t.Fatalf("ContextID = %q, want %q while a terminal tab is active", i.ContextID(), ctxTerminal)
	}
	if i.TabEditor(1) != nil {
		t.Fatal("TabEditor must be nil for a terminal tab")
	}
	if i.TabTerminal(1) != term || i.TabTerminal(0) != nil {
		t.Fatal("TabTerminal must return the terminal only for the terminal slot")
	}
}

// TestTerminalTabSkippedByEditorAccessors checks document sweeps never see
// terminal tabs.
func TestTerminalTabSkippedByEditorAccessors(t *testing.T) {
	i := editorInst(t)
	path := loadTab(t, i, "a.txt")
	termTab(t, i)
	if eds := i.Editors(); len(eds) != 1 {
		t.Fatalf("Editors() = %d entries, want 1 (terminal tabs skipped)", len(eds))
	}
	if idx := i.TabForPath(path); idx != 0 {
		t.Fatalf("TabForPath = %d, want 0", idx)
	}
	if i.EditorForPath(path) == nil {
		t.Fatal("EditorForPath must still find the file tab")
	}
}

// TestActivateBackToEditorTab verifies switching back restores the editor
// accessors and the editor context.
func TestActivateBackToEditorTab(t *testing.T) {
	i := editorInst(t)
	loadTab(t, i, "a.txt")
	termTab(t, i)
	if !i.ActivateTab(0) {
		t.Fatal("ActivateTab(0) refused")
	}
	if i.Editor() == nil || i.ActiveTerminal() != nil {
		t.Fatal("editor tab active: Editor() non-nil, ActiveTerminal() nil")
	}
	if i.ContextID() != ctxEditor {
		t.Fatalf("ContextID = %q, want %q", i.ContextID(), ctxEditor)
	}
}

// TestCloseTerminalTab verifies a terminal tab closes like any tab and the
// neighbour takes over.
func TestCloseTerminalTab(t *testing.T) {
	i := editorInst(t)
	loadTab(t, i, "a.txt")
	termTab(t, i)
	if !i.CloseTab(1) {
		t.Fatal("CloseTab(1) refused")
	}
	if i.TabCount() != 1 || i.Editor() == nil {
		t.Fatalf("after close: tabs=%d, editor=%v", i.TabCount(), i.Editor())
	}
}

// TestMoveMixedTabs verifies reordering across editor/terminal slots keeps the
// active tab pinned to its content.
func TestMoveMixedTabs(t *testing.T) {
	i := editorInst(t)
	loadTab(t, i, "a.txt")
	term := termTab(t, i)
	if !i.MoveTab(1, 0) {
		t.Fatal("MoveTab refused")
	}
	if i.TabTerminal(0) != term || i.ActiveTab() != 0 {
		t.Fatalf("after move: terminal at 0? %v active=%d", i.TabTerminal(0) == term, i.ActiveTab())
	}
	if i.TabEditor(1) == nil {
		t.Fatal("editor tab must sit at index 1 after the move")
	}
}

// TestAddTerminalTabOnlyOnEditors confirms non-editor instances refuse.
func TestAddTerminalTabOnlyOnEditors(t *testing.T) {
	r := newReg()
	r.AddExplorer()
	exp := r.Get(ExplorerKey)
	tm := terminal.New("terminal-test", "/nonexistent-shell-for-test", ".", 10, 4, nil, nil)
	if exp.AddTerminalTab(tm) != nil {
		t.Fatal("AddTerminalTab must refuse on the explorer")
	}
}

// TestMintTerminalKeyAdvances verifies tab-terminal keys never collide with
// terminal pane keys.
func TestMintTerminalKeyAdvances(t *testing.T) {
	r := newReg()
	if k := r.MintTerminalKey(); k != "terminal" {
		t.Fatalf("first minted key = %q, want terminal", k)
	}
	if k := r.MintTerminalKey(); k != "terminal:2" {
		t.Fatalf("second minted key = %q, want terminal:2", k)
	}
}

// TestReusableRunTerminal verifies the run-reuse scan (#574): the first
// never-typed-in terminal wins — pane or tab — and occupied ones are skipped.
func TestReusableRunTerminal(t *testing.T) {
	r := newReg()
	r.AddExplorer()
	ed := r.Get(r.AddEditor())
	if inst, _, _ := r.ReusableRunTerminal(); inst != nil {
		t.Fatal("no terminals: nothing to reuse")
	}
	term := termTab(t, ed)
	inst, idx, got := r.ReusableRunTerminal()
	if inst != ed || idx != 1 || got != term {
		t.Fatalf("expected the fresh terminal tab to be reusable (inst=%v idx=%d)", inst, idx)
	}
	// The test terminal's spawn failed (no live process), so it stays
	// reusable even after input — a dead terminal is always fair game; the
	// occupied-and-running skip is unit-tested in internal/terminal.
	term.PasteText("typed")
	if inst, _, _ := r.ReusableRunTerminal(); inst != ed {
		t.Fatal("a dead terminal must stay reusable")
	}
}

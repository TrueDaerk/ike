package app

import (
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// toolhide_test.go covers window.hideAllTools (#791): snapshot-hide every
// visible tool window, restore exactly, tolerate divergence while hidden.

// leafSet returns the current layout leaves as a set.
func leafSet(m Model) map[string]bool {
	out := map[string]bool{}
	for _, k := range layout.Leaves(m.activeWS().Tree) {
		out[k] = true
	}
	return out
}

func TestHideToolWindowsToggleRestoresExactly(t *testing.T) {
	m, termKey := openTestTerminal(t) // editor + explorer + terminal
	if !m.explorerVisible() {
		t.Fatal("fixture: explorer expected visible")
	}
	before := leavesSignature(m.activeWS().Tree)
	edKey := m.activeEditorKey()

	m = step(m, HideToolWindowsMsg{})
	leaves := leafSet(m)
	if leaves[pane.ExplorerKey] || leaves[termKey] {
		t.Fatalf("tools must be hidden, leaves = %v", leaves)
	}
	if !leaves[edKey] {
		t.Fatal("editor pane must survive")
	}
	if m.toolHide == nil {
		t.Fatal("snapshot must be stored")
	}
	// The terminal instance stays registered (session keeps running).
	if !m.activeWS().Panes.Has(termKey) {
		t.Fatal("hidden terminal must stay registered")
	}
	// Focus moved off the hidden terminal onto an editor.
	if got := m.activeWS().Panes.Focused(); got != edKey {
		t.Fatalf("focus = %q, want editor %q", got, edKey)
	}

	m = step(m, HideToolWindowsMsg{})
	if m.toolHide != nil {
		t.Fatal("restore must clear the snapshot")
	}
	if got := leavesSignature(m.activeWS().Tree); got != before {
		t.Fatalf("restore must bring the exact tree back:\n got %q\nwant %q", got, before)
	}
}

func TestHideToolWindowsNoToolsIsNoop(t *testing.T) {
	m := sized(t, 100, 40)
	// Hide the explorer first so only editors remain.
	m.hideExplorer()
	m = step(m, HideToolWindowsMsg{})
	if m.toolHide != nil {
		t.Fatal("no visible tools: no snapshot")
	}
}

func TestHideToolWindowsManualReopenWhileHidden(t *testing.T) {
	m, termKey := openTestTerminal(t)
	m = step(m, HideToolWindowsMsg{})
	// Re-open the explorer manually while hidden.
	m.showExplorer()
	m = step(m, HideToolWindowsMsg{}) // restore
	leaves := leafSet(m)
	if !leaves[pane.ExplorerKey] {
		t.Fatal("explorer must stay visible (no duplicate, no removal)")
	}
	if !leaves[termKey] {
		t.Fatal("terminal must be re-attached by the fallback")
	}
	// No duplicate explorer leaf.
	count := 0
	for _, k := range layout.Leaves(m.activeWS().Tree) {
		if k == pane.ExplorerKey {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("explorer leaf count = %d, want 1", count)
	}
}

func TestHideToolWindowsToolClosedWhileHidden(t *testing.T) {
	m, termKey := openTestTerminal(t)
	m = step(m, HideToolWindowsMsg{})
	// The terminal instance goes away while hidden.
	m.activeWS().Panes.Get(termKey).Terminal().Close()
	m.activeWS().Panes.Close(termKey)
	m = step(m, HideToolWindowsMsg{}) // restore
	leaves := leafSet(m)
	if leaves[termKey] {
		t.Fatal("a closed tool must not reappear")
	}
	if !leaves[pane.ExplorerKey] {
		t.Fatal("the surviving tool must come back")
	}
}

func TestHideToolWindowsEditorSplitChangedWhileHidden(t *testing.T) {
	m, termKey := openTestTerminal(t)
	m = step(m, HideToolWindowsMsg{})
	m = dispatch(t, m, SplitFocusedMsg{Zone: layout.ZoneRight}) // editor layout diverges
	m = step(m, HideToolWindowsMsg{})                           // restore falls back to re-attach
	leaves := leafSet(m)
	if !leaves[pane.ExplorerKey] || !leaves[termKey] {
		t.Fatalf("both tools must re-attach, leaves = %v", leaves)
	}
	if len(leaves) < 4 {
		t.Fatalf("editor split must survive the restore, leaves = %v", leaves)
	}
}

func TestHideAllToolsCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("window.hideAllTools"); !ok {
		t.Fatal("window.hideAllTools must be registered")
	}
}

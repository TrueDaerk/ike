package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/project"
)

// restore_focus_test.go covers #2491: restoring a layout whose explorer is
// hidden (no explorer leaf) must not focus the invisible explorer pane —
// telemetry showed window.restoreLayout dispatched right after project
// switches, the recovery from a keyboard parked in a pane nothing renders.

// writeLayout2491 plants a saved layout in root built from the given leaves,
// split left-to-right, with the given pane identities.
func writeLayout2491(t *testing.T, root string, leaves []string, ids map[string]paneIdentity) {
	t.Helper()
	var tree layout.Node = &layout.Leaf{Pane: leaves[0]}
	for _, key := range leaves[1:] {
		tree = &layout.Split{Orient: layout.Horizontal, Ratio: 0.5,
			A: tree, B: &layout.Leaf{Pane: key}}
	}
	encoded, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(persistedLayout{Tree: encoded, Panes: ids})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".ike"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ike", "layout.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertFocusVisible fails when the focused pane is not a leaf of the active
// layout tree — the #2491 breakage: keys route to the registry's focused
// instance, so an unrendered focus swallows every keystroke.
func assertFocusVisible(t *testing.T, m Model, label string) {
	t.Helper()
	focused := m.activeWS().Panes.Focused()
	for _, l := range layout.Leaves(m.activeWS().Tree) {
		if l == focused {
			return
		}
	}
	t.Fatalf("%s: focus %q is not a leaf of the layout %v", label, focused,
		layout.Leaves(m.activeWS().Tree))
}

// TestRestoreHiddenExplorerFocusesEditor (#2491): a layout saved with the
// explorer hidden and a bottom terminal restores with focus in the editor,
// not the invisible explorer — at startup, across a switch away and back
// (parked resume), and across a fresh re-restore after the workspace was
// dropped.
func TestRestoreHiddenExplorerFocusesEditor(t *testing.T) {
	a, b := twoRoots(t)
	writeLayout2491(t, a, []string{"editor", "terminal"}, map[string]paneIdentity{
		"editor":   {Kind: "editor"},
		"terminal": {Kind: "terminal"},
	})
	m := switchModel(t)
	defer closeLeafTerminals(m)
	assertFocusVisible(t, m, "startup restore")
	if got := m.activeWS().Panes.Focused(); got != "editor" {
		t.Fatalf("startup restore focus = %q, want the editor leaf", got)
	}

	// Switching into a project without layout.json builds the default set;
	// its explorer is visible and keeps the pre-#2491 focus.
	m = step(m, project.SwitchProjectMsg{Root: b})
	assertFocusVisible(t, m, "switch to bare project")

	// Back to the hidden-explorer project: the parked resume keeps the fixed
	// focus.
	m = step(m, project.SwitchProjectMsg{Root: a})
	assertFocusVisible(t, m, "parked resume")

	// Drop the parked workspace and switch back in: the restore-from-disk
	// path runs again, like resuming after a project.close.
	m = step(m, project.SwitchProjectMsg{Root: b})
	m.ws.Drop(a)
	m = step(m, project.SwitchProjectMsg{Root: a})
	assertFocusVisible(t, m, "fresh re-restore")
	if got := m.activeWS().Panes.Focused(); got != "editor" {
		t.Fatalf("re-restore focus = %q, want the editor leaf", got)
	}
}

// TestRestoreHiddenExplorerPrefersEditorOverFirstLeaf (#2491): when the first
// leaf in walk order is not an editor, focus still lands on the first
// editor-kind leaf rather than the terminal.
func TestRestoreHiddenExplorerPrefersEditorOverFirstLeaf(t *testing.T) {
	a, _ := twoRoots(t)
	writeLayout2491(t, a, []string{"terminal", "editor"}, map[string]paneIdentity{
		"terminal": {Kind: "terminal"},
		"editor":   {Kind: "editor"},
	})
	m := switchModel(t)
	defer closeLeafTerminals(m)
	if got := m.activeWS().Panes.Focused(); got != "editor" {
		t.Fatalf("focus = %q, want the editor leaf", got)
	}
}

// TestRestoreVisibleExplorerKeepsExplorerFocus (#2491): a layout that shows
// the explorer keeps the long-standing behavior — the tree gets the first
// focus, and a saved session's active file may then re-focus its editor.
func TestRestoreVisibleExplorerKeepsExplorerFocus(t *testing.T) {
	a, _ := twoRoots(t)
	writeLayout2491(t, a, []string{pane.ExplorerKey, "editor", "terminal"}, map[string]paneIdentity{
		pane.ExplorerKey: {Kind: "explorer"},
		"editor":         {Kind: "editor"},
		"terminal":       {Kind: "terminal"},
	})
	m := switchModel(t)
	defer closeLeafTerminals(m)
	if got := m.activeWS().Panes.Focused(); got != pane.ExplorerKey {
		t.Fatalf("focus = %q, want the explorer", got)
	}
}

package app

import (
	"testing"

	ilsp "ike/internal/lsp"
)

// goto_dedup_test.go covers #930: LSP navigation jumps (definition, usages —
// both funnel through ilsp.DefinitionMsg) focus the pane where the target
// file is already open instead of opening a duplicate tab.

func TestGotoDefinitionFocusesOtherPane(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	paneA := m.activeEditorKey()
	tm, _ = m.openPath(files[1], true) // second pane, focused
	m = tm.(Model)
	paneB := m.activeEditorKey()

	// Jump from paneB to a position in files[0], open in paneA.
	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[0], Line: 0, Col: 1})
	m = tm.(Model)
	if got := m.activeEditorKey(); got != paneA {
		t.Fatalf("jump must focus the pane holding the file: got %s, want %s", got, paneA)
	}
	if keys := m.editorKeysForPath(files[0]); len(keys) != 1 {
		t.Fatalf("no duplicate view expected, panes = %v", keys)
	}
	ed := m.editorForPath(files[0])
	if line, col := ed.CursorPos(); line != 0 || col != 1 {
		t.Fatalf("cursor = (%d,%d), want (0,1)", line, col)
	}
	// paneB keeps its own file.
	if inst := m.activeWS().Panes.Get(paneB); inst.TabForPath(files[0]) >= 0 {
		t.Fatal("paneB must not gain a tab for files[0]")
	}
}

func TestGotoDefinitionSamePaneActivatesTab(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	tm, _ = m.openPath(files[1], false) // second tab, same pane
	m = tm.(Model)
	key := m.activeEditorKey()

	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[0], Line: 0, Col: 1})
	m = tm.(Model)
	if got := m.activeEditorKey(); got != key {
		t.Fatalf("same-pane jump must stay in pane %s, got %s", key, got)
	}
	inst := m.activeWS().Panes.Get(key)
	if inst.TabCount() != 2 {
		t.Fatalf("tab count = %d, want 2 (no duplicate)", inst.TabCount())
	}
	if ed := inst.Editor(); ed == nil || ed.Path() != files[0] {
		t.Fatal("the files[0] tab must be active after the jump")
	}
}

func TestGotoDefinitionUnopenedOpensInCurrentPane(t *testing.T) {
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	m = tm.(Model)
	key := m.activeEditorKey()

	tm, _ = m.Update(ilsp.DefinitionMsg{Path: files[2], Line: 1, Col: 0})
	m = tm.(Model)
	if got := m.activeEditorKey(); got != key {
		t.Fatalf("an unopened target opens in the current pane %s, got %s", key, got)
	}
	inst := m.activeWS().Panes.Get(key)
	if inst.TabForPath(files[2]) < 0 {
		t.Fatal("files[2] must open as a tab in the current pane")
	}
}

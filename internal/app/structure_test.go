package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/structpanel"
)

// structure_test.go covers the Structure tool window wiring (#1025): the
// toggle state machine, the refresh triggers, symbol delivery, navigation
// through the open funnel, and layout persistence.

func structureSeed(t *testing.T) (Model, []string) {
	t.Helper()
	_, files := navProject(t)
	m := newSized()
	tm, _ := m.openPath(files[0], false)
	return tm.(Model), files
}

func sampleNodes() []ilsp.SymbolNode {
	return []ilsp.SymbolNode{
		{Name: "Top", Kind: 12, Line: 1, Col: 0, EndLine: 5},
		{Name: "Other", Kind: 12, Line: 7, Col: 0, EndLine: 9},
	}
}

func TestStructureToggleStateMachine(t *testing.T) {
	m, _ := structureSeed(t)
	editorKey := m.activeEditorKey()

	// Open: the pane appears and takes focus.
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	if !m.activeWS().Panes.Has(pane.StructureKey) {
		t.Fatal("toggle must open the structure pane")
	}
	if m.activeWS().Panes.Focused() != pane.StructureKey {
		t.Fatal("the fresh pane must hold focus")
	}

	// Focused → toggle returns focus to where it came from.
	tm, _ = m.Update(StructureToggleMsg{})
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != editorKey {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), editorKey)
	}
	if !m.activeWS().Panes.Has(pane.StructureKey) {
		t.Fatal("returning focus must not close the pane")
	}

	// Unfocused → toggle focuses the open pane.
	tm, _ = m.Update(StructureToggleMsg{})
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != pane.StructureKey {
		t.Fatal("toggle must re-focus the open pane")
	}
}

func TestStructureOpenRequestsSymbolsOnce(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	if m.structReqPath != files[0] {
		t.Fatalf("opening must issue a refresh for %q, requested %q", files[0], m.structReqPath)
	}
	// While the request is outstanding (or the server has no provider),
	// further settled passes must not re-request the same path.
	if m.structureNeedsRequest(m.structPanel().Path(), files[0]) {
		t.Fatal("an unanswered path must not re-request every pass")
	}
	// A save forces the re-request of the unchanged path.
	m.structForce = true
	if !m.structureNeedsRequest(m.structPanel().Path(), files[0]) {
		t.Fatal("a forced refresh must re-request the same path")
	}
}

func TestStructureSymbolsDeliveryAndFollow(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[0], Symbols: sampleNodes()})
	m = tm.(Model)
	sp := m.structPanel()
	if sp == nil || len(sp.Rows()) != 2 || sp.Path() != files[0] {
		t.Fatalf("delivery did not reach the panel: %+v", sp)
	}

	// Cursor follow: move the editor caret into "Other"; the settled pass
	// highlights the enclosing symbol.
	tm, _ = m.Update(StructureToggleMsg{}) // focus back to the editor
	m = tm.(Model)
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	ed.SetCursor(8, 0)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = tm.(Model)
	if got := m.structPanel().Current(); got != 1 {
		t.Fatalf("current = %d, want 1 (enclosing symbol)", got)
	}
}

func TestStructureNavigateMovesEditorAndRecordsHistory(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(structpanel.NavigateMsg{Path: files[0], Line: 7, Col: 2})
	m = tm.(Model)
	m = m.atPosition(t, files[0], 7)

	// The jump went through the open funnel, so nav.back returns to the origin.
	tm, _ = m.Update(NavBackMsg{})
	m = tm.(Model).atPosition(t, files[0], 0)
}

func TestStructureBufferSwitchRefreshes(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[0], Symbols: sampleNodes()})
	m = tm.(Model)

	// Opening another file re-targets the refresh on the next settled pass.
	tm, _ = m.openPath(files[1], false)
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = tm.(Model)
	if m.structReqPath != files[1] {
		t.Fatalf("switching buffers must request %q, requested %q", files[1], m.structReqPath)
	}
}

func TestStructureSaveForcesRefresh(t *testing.T) {
	m, files := structureSeed(t)
	tm, _ := m.Update(StructureToggleMsg{})
	m = tm.(Model)
	tm, _ = m.Update(ilsp.DocumentSymbolsMsg{Path: files[0], Symbols: sampleNodes()})
	m = tm.(Model)
	if m.structForce {
		t.Fatal("setup: no forced refresh pending")
	}
	tm, _ = m.Update(todoSavedMsg{path: files[0]})
	m = tm.(Model)
	// The wrapper's sync consumed the force flag and re-issued the request.
	if m.structForce {
		t.Fatal("the forced refresh must be consumed by the settled pass")
	}
	if m.structReqPath != files[0] {
		t.Fatalf("save must re-request %q, requested %q", files[0], m.structReqPath)
	}
}

func TestStructurePanePersists(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	out, _ = m.Update(StructureToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.StructureKey) {
		t.Fatal("setup: pane not open")
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(pane.StructureKey)
	if inst == nil || inst.Kind() != pane.KindStructure {
		t.Fatal("panel did not restore")
	}
	if inst.Structure().Path() != "" {
		t.Fatal("the panel restores empty; the first sync refills it")
	}
}

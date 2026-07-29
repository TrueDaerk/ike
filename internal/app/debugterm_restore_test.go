package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// TestDebugTerminalPersistsAsDebugTermAndPrunesOnRestore (#1370): the pane
// pair persists — the panel with its "debug" identity, the debuggee terminal
// as "debugTerm" — and a restart restores the panel empty while the terminal
// leaf is pruned (its content is session state; a shell in its place would be
// misleading).
func TestDebugTerminalPersistsAsDebugTermAndPrunesOnRestore(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)

	m, _, path := debugModel(t)
	t.Setenv("IKE_CONFIG_DIR", store) // debugModel redirects; pin it back
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2, Column: 1}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	if m.debugTermInstance() == nil {
		t.Fatal("precondition: the pane pair is open")
	}
	termKey := m.dbgTermKey
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)

	data, err := os.ReadFile(filepath.Join(store, "layout.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p persistedLayout
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	if p.Panes[pane.DebugKey].Kind != "debug" {
		t.Fatalf("panel identity = %q, want debug", p.Panes[pane.DebugKey].Kind)
	}
	if p.Panes[termKey].Kind != "debugTerm" {
		t.Fatalf("terminal identity = %q, want debugTerm", p.Panes[termKey].Kind)
	}

	m2 := NewWith(registry.New(), host.MapConfig{})
	out, _ := m2.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 = out.(Model)
	if !m2.activeWS().Panes.Has(pane.DebugKey) {
		t.Fatal("the debug panel must restore empty in its slot")
	}
	if m2.activeWS().Panes.Has(termKey) {
		t.Fatal("the debuggee terminal must not resurrect")
	}
	for _, leaf := range layout.Leaves(m2.activeWS().Tree) {
		if leaf == termKey {
			t.Fatal("the debuggee terminal leaf must be pruned from the tree")
		}
	}
}

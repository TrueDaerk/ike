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

// TestDebugAreaPersistsAndRestoresEmpty (#1370 → #2190): the combined debug
// area persists as one "debug" leaf — the embedded console is session state
// and leaves no identity of its own — and a restart restores the panel empty
// (no console, variables view) in its slot.
func TestDebugAreaPersistsAndRestoresEmpty(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)

	m, _, path := debugModel(t)
	t.Setenv("IKE_CONFIG_DIR", store) // debugModel redirects; pin it back
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2, Column: 1}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	if m.debugConsole() == nil {
		t.Fatal("precondition: the debug area is open with its console")
	}
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
		t.Fatalf("area identity = %q, want debug", p.Panes[pane.DebugKey].Kind)
	}
	for key, id := range p.Panes {
		if id.Kind == "debugTerm" {
			t.Fatalf("no leaf may persist as debugTerm anymore, got %q", key)
		}
	}

	m2 := NewWith(registry.New(), host.MapConfig{})
	out, _ := m2.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 = out.(Model)
	if !m2.activeWS().Panes.Has(pane.DebugKey) {
		t.Fatal("the debug area must restore empty in its slot")
	}
	if p := m2.activeWS().Panes.Get(pane.DebugKey).Debug(); p.HasTerm() {
		t.Fatal("the restored area must carry no console — that is session state")
	}
}

// TestLegacyDebugTermLeafPrunesOnRestore: a layout.json written before #2190
// still carries the separate debuggee terminal leaf as "debugTerm"; the
// restore drops it instead of spawning a shell there.
func TestLegacyDebugTermLeafPrunesOnRestore(t *testing.T) {
	store := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", store)

	tree := &layout.Split{Orient: layout.Horizontal, Ratio: 0.3,
		A: &layout.Leaf{Pane: "explorer"},
		B: &layout.Split{Orient: layout.Horizontal, Ratio: 0.5,
			A: &layout.Leaf{Pane: pane.DebugKey}, B: &layout.Leaf{Pane: "term:9"}}}
	encoded, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	saved := persistedLayout{
		Tree: encoded,
		Panes: map[string]paneIdentity{
			"explorer":    {Kind: "explorer"},
			pane.DebugKey: {Kind: "debug"},
			"term:9":      {Kind: "debugTerm"},
		},
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "layout.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = out.(Model)
	if m.activeWS().Panes.Has("term:9") {
		t.Fatal("the legacy debuggee terminal must not resurrect")
	}
	for _, leaf := range layout.Leaves(m.activeWS().Tree) {
		if leaf == "term:9" {
			t.Fatal("the legacy debuggee terminal leaf must be pruned from the tree")
		}
	}
	if !m.activeWS().Panes.Has(pane.DebugKey) {
		t.Fatal("the debug area itself must survive the legacy prune")
	}
}

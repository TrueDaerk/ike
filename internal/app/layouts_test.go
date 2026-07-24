package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/layout"
	"ike/internal/palette"
	"ike/internal/pane"

	"ike/internal/explorer"
)

// layouts_test.go covers saved window layouts (#1175): the user-scoped store,
// the kind-only snapshot, the save prompt, the instance-preserving apply, and
// the default-layout fallback for new projects.

// typeText feeds each rune of s as a key press.
func typeText(m Model, s string) Model {
	for _, r := range s {
		out, _ := m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
		m = out.(Model)
	}
	return m
}

// twoPaneSnapshot hand-builds a persisted explorer+editor layout.
func twoPaneSnapshot(t *testing.T) persistedLayout {
	t.Helper()
	tree := &layout.Split{Orient: layout.Horizontal, Ratio: 0.25,
		A: &layout.Leaf{Pane: "explorer"}, B: &layout.Leaf{Pane: "editor"}}
	data, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	return persistedLayout{Tree: data, Panes: map[string]paneIdentity{
		"explorer": {Kind: "explorer"},
		"editor":   {Kind: "editor"},
	}}
}

// threePaneSnapshot hand-builds explorer + two stacked editor slots.
func threePaneSnapshot(t *testing.T) persistedLayout {
	t.Helper()
	tree := &layout.Split{Orient: layout.Horizontal, Ratio: 0.25,
		A: &layout.Leaf{Pane: "explorer"},
		B: &layout.Split{Orient: layout.Vertical, Ratio: 0.5,
			A: &layout.Leaf{Pane: "editor"}, B: &layout.Leaf{Pane: "editor:2"}}}
	data, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	return persistedLayout{Tree: data, Panes: map[string]paneIdentity{
		"explorer": {Kind: "explorer"},
		"editor":   {Kind: "editor"},
		"editor:2": {Kind: "editor"},
	}}
}

func TestLayoutStoreRoundTrip(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	s := savedLayouts{Layouts: map[string]persistedLayout{"dev": twoPaneSnapshot(t)}, Default: "dev"}
	saveUserLayouts(s)
	got := loadUserLayouts()
	if got.Default != "dev" {
		t.Fatalf("Default = %q, want dev", got.Default)
	}
	if _, ok := got.Layouts["dev"]; !ok {
		t.Fatal("saved layout missing after reload")
	}
	names, def := layoutNames()
	if len(names) != 1 || names[0] != "dev" || def != "dev" {
		t.Fatalf("layoutNames() = %v, %q", names, def)
	}
}

func TestDeleteLayoutClearsDefault(t *testing.T) {
	m, _ := openTestTerminal(t)
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": twoPaneSnapshot(t)}, Default: "dev"})
	m = step(m, DeleteLayoutMsg{Name: "dev"})
	got := loadUserLayouts()
	if len(got.Layouts) != 0 || got.Default != "" {
		t.Fatalf("delete must drop the layout and its default marker, got %+v", got)
	}
}

func TestSnapshotLayoutStripsToKinds(t *testing.T) {
	m, termKey := openTestTerminal(t) // explorer + editor (+ file?) + terminal
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	tree, leaves, ok := layout.DecodeTree(snap.Tree)
	if !ok || tree == nil {
		t.Fatal("snapshot tree does not decode")
	}
	if len(leaves) != len(layout.Leaves(m.activeWS().Tree)) {
		t.Fatalf("leaf count changed: %d != %d", len(leaves), len(layout.Leaves(m.activeWS().Tree)))
	}
	kinds := map[string]int{}
	for _, key := range leaves {
		id, ok := snap.Panes[key]
		if !ok {
			t.Fatalf("leaf %q has no identity", key)
		}
		kinds[id.Kind]++
		if id.Path != "" || id.Path2 != "" || len(id.Tabs) != 0 || len(id.Pinned) != 0 || id.Active != 0 {
			t.Fatalf("identity of %q must be kind-only, got %+v", key, id)
		}
	}
	if kinds["explorer"] != 1 || kinds["editor"] < 1 || kinds["terminal"] != 1 {
		t.Fatalf("unexpected kind histogram: %v (terminal was %q)", kinds, termKey)
	}
}

func TestSaveLayoutPromptSavesAndGuardsOverwrite(t *testing.T) {
	m := sized(t, 100, 40)
	m = step(m, SaveLayoutPromptMsg{})
	if !m.layoutSavePromptOpen() {
		t.Fatal("prompt must be open")
	}
	m = typeText(m, "dev")
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.layoutSavePromptOpen() {
		t.Fatal("prompt must close after save")
	}
	if _, ok := loadUserLayouts().Layouts["dev"]; !ok {
		t.Fatal("layout dev must be saved")
	}
	// Saving the same name again requires a confirming second enter.
	m = step(m, SaveLayoutPromptMsg{})
	m = typeText(m, "dev")
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.layoutSavePromptOpen() || m.layoutSaveErr == "" {
		t.Fatal("existing name must arm the overwrite guard, not save")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.layoutSavePromptOpen() {
		t.Fatal("second enter must overwrite and close")
	}
}

func TestApplyLayoutMergesSurplusEditors(t *testing.T) {
	dir := t.TempDir()
	pa := filepath.Join(dir, "a.txt")
	pb := filepath.Join(dir, "b.txt")
	for _, p := range []string{pa, pb} {
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := sized(t, 100, 40)
	m = step(m, explorer.OpenFileMsg{Path: pa})
	m = step(m, SplitFocusedMsg{Zone: layout.ZoneRight}) // second editor pane, focused
	m = step(m, explorer.OpenFileMsg{Path: pb})
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"one": twoPaneSnapshot(t)}})

	m = step(m, ApplyLayoutMsg{Name: "one"})

	leaves := layout.Leaves(m.activeWS().Tree)
	if len(leaves) != 2 {
		t.Fatalf("applied layout must have 2 leaves, got %v", leaves)
	}
	var ed *pane.Instance
	for _, key := range leaves {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			ed = inst
		}
	}
	if ed == nil {
		t.Fatal("no editor slot after apply")
	}
	if ed.TabForPath(pa) < 0 || ed.TabForPath(pb) < 0 {
		t.Fatalf("both files must survive the merge; tabs = %d", ed.TabCount())
	}
}

func TestApplyLayoutKeepsHiddenToolRegistered(t *testing.T) {
	m, termKey := openTestTerminal(t)
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"plain": twoPaneSnapshot(t)}})
	m = step(m, ApplyLayoutMsg{Name: "plain"})
	leaves := leafSet(m)
	if leaves[termKey] {
		t.Fatal("terminal leaf must be gone")
	}
	if !m.activeWS().Panes.Has(termKey) {
		t.Fatal("terminal instance must stay registered (session keeps running)")
	}
	if !leaves[pane.ExplorerKey] {
		t.Fatal("explorer must be in the applied layout")
	}
}

func TestApplyLayoutFillsMissingSlotsWithScratch(t *testing.T) {
	m := sized(t, 100, 40) // one editor pane only
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"wide": threePaneSnapshot(t)}})
	m = step(m, ApplyLayoutMsg{Name: "wide"})
	editors := 0
	for _, key := range layout.Leaves(m.activeWS().Tree) {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			editors++
		}
	}
	if editors != 2 {
		t.Fatalf("layout with two editor slots must yield two editor panes, got %d", editors)
	}
}

func TestRestoreLayoutFallsBackToDefaultLayout(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"wide": threePaneSnapshot(t)}, Default: "wide"})
	m := New() // fresh project: no layout.json in the redirected store
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	leaves := layout.Leaves(m.activeWS().Tree)
	if len(leaves) != 3 {
		t.Fatalf("new project must materialize the default layout (3 leaves), got %v", leaves)
	}
	// The project's own persisted layout still wins on the next start.
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	if _, _, ok := loadLayout(); !ok {
		t.Fatal("project layout must persist after the default materialized")
	}
}

func TestRestoreDefaultLayoutBuiltIn(t *testing.T) {
	m, termKey := openTestTerminal(t) // no saved layouts, no default
	m = step(m, RestoreDefaultLayoutMsg{})
	leaves := leafSet(m)
	if len(leaves) != 2 || !leaves[pane.ExplorerKey] {
		t.Fatalf("built-in default must be explorer+editor, got %v", leaves)
	}
	if !m.activeWS().Panes.Has(termKey) {
		t.Fatal("terminal must stay registered after restore")
	}
}

func TestLayoutsModeResults(t *testing.T) {
	mode := newLayoutsMode(func() ([]string, string) { return []string{"b", "a"}, "a" })
	items := mode.Results("", palette.Context{})
	if len(items) != 2 || items[0].Title != "a" || items[1].Title != "b" {
		t.Fatalf("names must list sorted, got %+v", items)
	}
	if items[0].Detail != "default" || items[1].Detail != "" {
		t.Fatal("default marker must sit on the default row only")
	}
	if _, ok := items[0].Msg.(ApplyLayoutMsg); !ok {
		t.Fatalf("enter must apply, got %T", items[0].Msg)
	}
	if _, ok := items[0].Aux.(DeleteLayoutMsg); !ok {
		t.Fatalf("aux must delete, got %T", items[0].Aux)
	}
	mode.setDefault = true
	items = mode.Results("", palette.Context{})
	if _, ok := items[0].Msg.(SetDefaultLayoutMsg); !ok {
		t.Fatalf("set-default open must emit SetDefaultLayoutMsg, got %T", items[0].Msg)
	}
}

func TestSetDefaultLayout(t *testing.T) {
	m := sized(t, 100, 40)
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": twoPaneSnapshot(t)}})
	m = step(m, SetDefaultLayoutMsg{Name: "dev"})
	if loadUserLayouts().Default != "dev" {
		t.Fatal("default marker must be set")
	}
	m = step(m, SetDefaultLayoutMsg{Name: "missing"})
	if loadUserLayouts().Default != "dev" {
		t.Fatal("an unknown name must not change the default")
	}
	_ = m
}

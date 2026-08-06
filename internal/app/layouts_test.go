package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/layout"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/terminal"

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
	// The pane selection (#1568) precedes the prompt; enter keeps all panes.
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
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
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
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

func TestApplyLayoutKeepsSurplusEditorsAsPanes(t *testing.T) {
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

	// The surplus editor grafts into the implicit flexible region (#1577):
	// both files stay visible in their own panes, nothing merges into tabs.
	leaves := layout.Leaves(m.activeWS().Tree)
	if len(leaves) != 3 {
		t.Fatalf("surplus editor must keep its own pane (3 leaves), got %v", leaves)
	}
	panesWithFile := map[string]int{}
	for _, key := range leaves {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, p := range []string{pa, pb} {
			if inst.TabForPath(p) >= 0 {
				panesWithFile[p]++
			}
		}
	}
	if panesWithFile[pa] != 1 || panesWithFile[pb] != 1 {
		t.Fatalf("each file must sit in its own pane, got %v", panesWithFile)
	}
}

// TestApplyLayoutPreservesLeftoverArrangement checks that the leftover panes
// graft in their pre-apply relative arrangement (#1577): a stacked pair stays
// stacked instead of flattening into a row of siblings.
func TestApplyLayoutPreservesLeftoverArrangement(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{}
	for _, name := range []string{"a", "b", "c"} {
		p := filepath.Join(dir, name+".txt")
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths[name] = p
	}
	m := sized(t, 100, 40)
	m = step(m, explorer.OpenFileMsg{Path: paths["a"]})
	m = step(m, SplitFocusedMsg{Zone: layout.ZoneRight})
	m = step(m, explorer.OpenFileMsg{Path: paths["b"]})
	m = step(m, SplitFocusedMsg{Zone: layout.ZoneBottom}) // c below b
	m = step(m, explorer.OpenFileMsg{Path: paths["c"]})
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"one": twoPaneSnapshot(t)}})

	m = step(m, ApplyLayoutMsg{Name: "one"})

	rects := layout.Compute(m.activeWS().Tree, layout.Rect{W: 100, H: 40}).Panes
	rectFor := func(name string) layout.Rect {
		for _, key := range layout.Leaves(m.activeWS().Tree) {
			inst := m.activeWS().Panes.Get(key)
			if inst != nil && inst.Kind() == pane.KindEditor && inst.TabForPath(paths[name]) >= 0 {
				return rects[key]
			}
		}
		t.Fatalf("no pane shows %s", name)
		return layout.Rect{}
	}
	rb, rc := rectFor("b"), rectFor("c")
	if rb.Y+rb.H > rc.Y {
		t.Fatalf("b must stay stacked above c, got b=%+v c=%+v", rb, rc)
	}
	if rb.X != rc.X {
		t.Fatalf("stacked pair must keep its column, got b=%+v c=%+v", rb, rc)
	}
}

// terminalTabs collects the terminal tab models hosted by inst.
func terminalTabs(inst *pane.Instance) []*terminal.Model {
	var out []*terminal.Model
	for i := 0; i < inst.TabCount(); i++ {
		if tm := inst.TabTerminal(i); tm != nil {
			out = append(out, tm)
		}
	}
	return out
}

// editorTerminalTabs collects the terminal tabs across every editor-kind leaf.
func editorTerminalTabs(m Model) []*terminal.Model {
	var out []*terminal.Model
	for _, key := range layout.Leaves(m.activeWS().Tree) {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			out = append(out, terminalTabs(inst)...)
		}
	}
	return out
}

func TestApplyLayoutKeepsOrphanShellAsPane(t *testing.T) {
	m, termKey := openTestTerminal(t)
	sessKey := m.activeWS().Panes.Get(termKey).Terminal().SessionKey()
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"plain": twoPaneSnapshot(t)}})
	m = step(m, ApplyLayoutMsg{Name: "plain"})
	leaves := leafSet(m)
	if !leaves[pane.ExplorerKey] {
		t.Fatal("explorer must be in the applied layout")
	}
	// The shell has no slot in the layout: it grafts into the implicit
	// flexible region as its own pane (#1577) — reachable (#1275), no tab.
	if !leaves[termKey] {
		t.Fatalf("shell must keep its own pane, leaves = %v", leaves)
	}
	inst := m.activeWS().Panes.Get(termKey)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatal("shell pane must stay a terminal pane")
	}
	tm := inst.Terminal()
	t.Cleanup(tm.Close)
	if !tm.Running() || tm.SessionKey() != sessKey {
		t.Fatal("grafted pane must host the original live session")
	}
	if tabs := editorTerminalTabs(m); len(tabs) != 0 {
		t.Fatalf("no editor pane may swallow the shell as a tab, got %d", len(tabs))
	}
}

// termSlotSnapshot hand-builds explorer + editor + one terminal slot.
func termSlotSnapshot(t *testing.T) persistedLayout {
	t.Helper()
	tree := &layout.Split{Orient: layout.Horizontal, Ratio: 0.25,
		A: &layout.Leaf{Pane: "explorer"},
		B: &layout.Split{Orient: layout.Vertical, Ratio: 0.7,
			A: &layout.Leaf{Pane: "editor"}, B: &layout.Leaf{Pane: "terminal"}}}
	data, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	return persistedLayout{Tree: data, Panes: map[string]paneIdentity{
		"explorer": {Kind: "explorer"},
		"editor":   {Kind: "editor"},
		"terminal": {Kind: "terminal"},
	}}
}

func TestApplyLayoutKeepsSurplusShellAsPane(t *testing.T) {
	m, key1 := openTestTerminal(t)
	m = step(m, TerminalNewMsg{})
	key2 := m.activeWS().Panes.Focused()
	inst2 := m.activeWS().Panes.Get(key2)
	if inst2 == nil || inst2.Kind() != pane.KindTerminal {
		t.Fatalf("second terminal.new should focus a terminal pane, got %q", key2)
	}
	t.Cleanup(func() { inst2.Terminal().Close() })
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": termSlotSnapshot(t)}})

	m = step(m, ApplyLayoutMsg{Name: "dev"})

	leaves := leafSet(m)
	if !leaves[key1] {
		t.Fatal("first shell must fill the layout's terminal slot")
	}
	// The second shell has no slot: it keeps its own pane in the implicit
	// flexible region (#1577); the slot never converts to a tab host.
	if !leaves[key2] {
		t.Fatalf("surplus shell must keep its own pane, leaves = %v", leaves)
	}
	for _, key := range []string{key1, key2} {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindTerminal {
			t.Fatalf("%q must stay a terminal pane", key)
		}
		tm := inst.Terminal()
		t.Cleanup(tm.Close)
		if !tm.Running() {
			t.Fatal("both shell sessions must keep running")
		}
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
	if !leaves[pane.ExplorerKey] {
		t.Fatalf("built-in default must contain the explorer, got %v", leaves)
	}
	// The shell keeps its own pane in the implicit flexible region (#1577).
	if len(leaves) != 3 || !leaves[termKey] {
		t.Fatalf("shell must survive the restore as its own pane, got %v", leaves)
	}
	inst := m.activeWS().Panes.Get(termKey)
	if inst == nil || inst.Kind() != pane.KindTerminal || !inst.Terminal().Running() {
		t.Fatal("shell session must keep running in its pane")
	}
	t.Cleanup(inst.Terminal().Close)
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

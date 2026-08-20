package app

import (
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// toolhost_layout_test.go covers #1989: a pane hosting nothing but tool tabs
// is the layout's tools area, not an editor slot. It persists under its own
// kind ("tools"), legacy "editor"+Tools files keep loading, and opening a
// file with no live editor recreates the editor in the designated layout's
// editor slot instead of splitting next to the tools pane.

func TestToolsKindRoundTripsThroughLayout(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	t.Chdir(dir)
	toolLayout(t, conf, paneIdentity{Kind: "tools", Tools: []string{"alpha", "beta"}})

	m := fixedDirApp(t, conf)
	inst := m.activeWS().Panes.Get("editor")
	if inst == nil || inst.Kind() != pane.KindEditor || inst.TabCount() != 2 {
		t.Fatalf("tools pane must restore as a two-tab host, got %+v", inst)
	}
	for i, want := range []string{"alpha", "beta"} {
		tt := inst.TabTerminal(i)
		if tt == nil || tt.Tool() != want {
			t.Fatalf("tab %d must host tool %q", i, want)
		}
	}
	if !toolTabHost(inst) {
		t.Fatal("the restored pane must count as a tool-tab host")
	}

	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	id := readToolLayout(t, conf)
	if id.Kind != "tools" || len(id.Tools) != 2 {
		t.Fatalf("re-save must keep the tools kind, got %+v", id)
	}
	closeLeafTerminals(m)
}

func TestLegacyEditorToolsLayoutLoadsAndMigrates(t *testing.T) {
	conf, dir := t.TempDir(), t.TempDir()
	t.Chdir(dir)
	// A pre-#1989 file: the tool-tab host saved as a plain editor identity.
	toolLayout(t, conf, paneIdentity{Kind: "editor", Tools: []string{"alpha", "beta"}})

	m := fixedDirApp(t, conf)
	inst := m.activeWS().Panes.Get("editor")
	if inst == nil || inst.Kind() != pane.KindEditor || inst.TabCount() != 2 {
		t.Fatalf("legacy tools pane must restore as a two-tab host, got %+v", inst)
	}

	// The next save migrates the identity to the distinguishing kind.
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	id := readToolLayout(t, conf)
	if id.Kind != "tools" {
		t.Fatalf("re-save must migrate the legacy shape to kind tools, got %q", id.Kind)
	}
	if len(id.Tools) != 2 || id.Tools[0] != "alpha" || id.Tools[1] != "beta" {
		t.Fatalf("migration must keep the tool list, got %v", id.Tools)
	}
	closeLeafTerminals(m)
}

// repro1989Layout stores the #1989 repro layout as the designated default:
// explorer|editor on top, a tool pane and a tool-tab host below.
func repro1989Layout(t *testing.T) {
	t.Helper()
	tree := &layout.Split{Orient: layout.Vertical, Ratio: 2.0 / 3,
		A: &layout.Split{Orient: layout.Horizontal, Ratio: 0.12,
			A: &layout.Leaf{Pane: "explorer"}, B: &layout.Leaf{Pane: "editor"}},
		B: &layout.Split{Orient: layout.Horizontal, Ratio: 0.5,
			A: &layout.Leaf{Pane: "terminal"}, B: &layout.Leaf{Pane: "editor:2"}}}
	data, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	saveUserLayouts(savedLayouts{Default: "dev", Layouts: map[string]persistedLayout{"dev": {
		Tree: data,
		Panes: map[string]paneIdentity{
			"explorer": {Kind: "explorer"},
			"editor":   {Kind: "editor"},
			"terminal": {Kind: "tool", Tool: "alpha"},
			"editor:2": {Kind: "tools", Tools: []string{"beta", "gamma"}},
		},
	}}})
}

// TestOpenFileRecreatesEditorAtLayoutSlot pins the #1989 repro: with the last
// editor closed, opening a file must recreate the editor in the layout's
// editor slot — next to the explorer — never next to the tool-tab pane.
func TestOpenFileRecreatesEditorAtLayoutSlot(t *testing.T) {
	withTools(t, sleepTool("alpha"), sleepTool("beta"), sleepTool("gamma"))
	m := sized(t, 120, 40)
	repro1989Layout(t)
	m = step(m, ApplyLayoutMsg{Name: "dev"})

	// Close the layout's only real editor pane; only explorer, the tool pane
	// and the tool-tab host remain.
	edKey := m.fileEditorKey()
	if edKey == "" {
		t.Fatal("the applied layout must have a file-editing pane")
	}
	m.closePane(edKey)
	if key := m.fileEditorKey(); key != "" {
		t.Fatalf("no pane may edit files after the close, got %q", key)
	}

	dir := t.TempDir()
	f := writeTemp(t, dir, "x.txt", "hello\n")
	tm, _ := m.openPath(f, false)
	m = tm.(Model)

	newKey := m.editorWithFile(f)
	if newKey == "" {
		t.Fatal("the file must be open in an editor pane")
	}
	sp := parentSplit(m.activeWS().Tree, newKey)
	if sp == nil {
		t.Fatal("the new editor must be a split child")
	}
	sib, ok := sp.A.(*layout.Leaf)
	if !ok || sib.Pane != pane.ExplorerKey {
		t.Fatalf("the new editor must split off the explorer (its layout slot), sibling=%v", sp.A)
	}
	if sp.Orient != layout.Horizontal {
		t.Fatalf("the editor slot sits right of the explorer, got orient %v", sp.Orient)
	}
	if sp.Ratio < 0.11 || sp.Ratio > 0.13 {
		t.Fatalf("the saved split share must carry over, got %v", sp.Ratio)
	}
	closeLeafTerminals(m)
}

func TestSnapshotMultiToolHostAsToolsKind(t *testing.T) {
	withTools(t, sleepTool("alpha"), sleepTool("beta"))
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "alpha"})
	m = step(m, ToolOpenMsg{Name: "beta"})
	host := m.toolPane("beta")
	if host == nil || !host.ConvertToTabHost() {
		t.Fatal("tool pane must convert to a tab host")
	}
	m.adoptTerminalPane(m.toolPane("alpha").Key(), host.Key())
	if !toolTabHost(host) {
		t.Fatalf("two tool tabs and no files must be a tool-tab host, tabs=%d", host.TabCount())
	}

	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	toolsSlots, editorToolSlots := 0, 0
	for _, id := range snap.Panes {
		switch {
		case id.Kind == "tools":
			toolsSlots++
			if len(id.Tools) != 2 {
				t.Fatalf("the tools slot must keep both tool names, got %v", id.Tools)
			}
		case id.Kind == "editor" && len(id.Tools) > 0:
			editorToolSlots++
		}
	}
	if toolsSlots != 1 || editorToolSlots != 0 {
		t.Fatalf("a pure tool host must snapshot as kind tools, got tools=%d editor+tools=%d", toolsSlots, editorToolSlots)
	}

	// Apply re-slots the live host into the tools slot: same pane, no
	// duplicate tool processes, and no editor slot consumed by it.
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})
	hostKey := host.Key()
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	if !leafSet(m)[hostKey] {
		t.Fatal("the live tool host must re-slot into the tools slot")
	}
	total := 0
	for key := range leafSet(m) {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			if inst.Terminal().Tool() != "" {
				total++
			}
		case pane.KindEditor:
			for i := 0; i < inst.TabCount(); i++ {
				if tt := inst.TabTerminal(i); tt != nil && tt.Tool() != "" {
					total++
				}
			}
		}
	}
	if total != 2 {
		t.Fatalf("apply must not restart or duplicate tools, got %d tool sessions", total)
	}
	closeLeafTerminals(m)
}

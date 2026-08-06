package app

import (
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// layouts_tools_test.go covers tool panes in saved layouts (#1277): a tool
// hosted as a tab (#836) must survive the kind-only snapshot and restart on
// apply instead of degrading to a plain editor slot.

// leafToolCounts tallies dedicated tool panes and editor-kind leaves across
// the applied tree.
func leafToolCounts(m Model) (tools map[string]int, editors int) {
	tools = map[string]int{}
	for key := range leafSet(m) {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			tools[inst.Terminal().Tool()]++
		case pane.KindEditor:
			editors++
		}
	}
	return tools, editors
}

func TestApplyLayoutRestoresTwoToolSlots(t *testing.T) {
	withTools(t, sleepTool("alpha"), sleepTool("beta"))
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "alpha"})
	m = step(m, ToolOpenMsg{Name: "beta"})
	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})

	// Round trip: tools still open, apply — both re-slot live.
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	tools, _ := leafToolCounts(m)
	if tools["alpha"] != 1 || tools["beta"] != 1 {
		t.Fatalf("both live tools must re-slot, got %v", tools)
	}

	// Restart path: close both panes, apply again — both restart fresh.
	for _, name := range []string{"alpha", "beta"} {
		if inst := m.toolPane(name); inst != nil {
			m.activeWS().Panes.Close(inst.Key())
		}
	}
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	tools, _ = leafToolCounts(m)
	if tools["alpha"] != 1 || tools["beta"] != 1 {
		t.Fatalf("both tools must restart in their slots, got %v", tools)
	}
	for key := range leafSet(m) {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindTerminal {
			t.Cleanup(inst.Terminal().Close)
		}
	}
}

func TestSnapshotToolTabHostAsToolSlot(t *testing.T) {
	withTools(t, sleepTool("alpha"), sleepTool("beta"))
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "alpha"})
	m = step(m, ToolOpenMsg{Name: "beta"})
	// The second tool becomes a tab host (#836), KindEditor from here on.
	host := m.toolPane("beta")
	if !host.ConvertToTabHost() {
		t.Fatal("tool pane must convert to a tab host")
	}

	snap, ok := snapshotLayout(m.activeWS().Tree, m.activeWS().Panes)
	if !ok {
		t.Fatal("snapshot failed")
	}
	kinds := map[string]int{}
	for _, id := range snap.Panes {
		if id.Kind == "tool" {
			kinds[id.Tool]++
		}
	}
	if kinds["alpha"] != 1 || kinds["beta"] != 1 {
		t.Fatalf("both tools must snapshot as tool slots, got %v", kinds)
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": snap}})

	// Close everything running, apply: both tools restart as dedicated panes.
	m.activeWS().Panes.Close(m.toolPane("alpha").Key())
	host.CloseTerminalTabs()
	m.activeWS().Panes.Close(host.Key())
	m = step(m, ApplyLayoutMsg{Name: "dev"})
	tools, _ := leafToolCounts(m)
	if tools["alpha"] != 1 || tools["beta"] != 1 {
		t.Fatalf("tab-hosted tool must restore as a tool slot, got %v", tools)
	}
	for key := range leafSet(m) {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindTerminal {
			t.Cleanup(inst.Terminal().Close)
		}
	}
}

// TestApplyLayoutNeverGraftsShellIntoToolPane pins the #1577 report: a saved
// layout whose only terminal-kind slot is a tool pane must not swallow a
// surplus plain shell as a tab (converting the tool into a tab host) — the
// shell keeps its own pane in the implicit flexible region.
func TestApplyLayoutNeverGraftsShellIntoToolPane(t *testing.T) {
	withTools(t, sleepTool("alpha"))
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "alpha"})
	m = step(m, TerminalNewMsg{})
	shellKey := m.activeWS().Panes.Focused()
	shellInst := m.activeWS().Panes.Get(shellKey)
	if shellInst == nil || shellInst.Kind() != pane.KindTerminal || shellInst.Terminal().Tool() != "" {
		t.Fatalf("expected a plain shell pane, got %q", shellKey)
	}
	t.Cleanup(shellInst.Terminal().Close)

	tree := &layout.Split{Orient: layout.Horizontal, Ratio: 0.25,
		A: &layout.Leaf{Pane: "explorer"},
		B: &layout.Split{Orient: layout.Vertical, Ratio: 0.7,
			A: &layout.Leaf{Pane: "editor"}, B: &layout.Leaf{Pane: "terminal"}}}
	data, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": {
		Tree: data,
		Panes: map[string]paneIdentity{
			"explorer": {Kind: "explorer"},
			"editor":   {Kind: "editor"},
			"terminal": {Kind: "tool", Tool: "alpha"},
		},
	}}})

	m = step(m, ApplyLayoutMsg{Name: "dev"})

	tool := m.toolPane("alpha")
	if tool == nil {
		t.Fatal("tool pane must survive the apply as a dedicated tool pane")
	}
	t.Cleanup(tool.Terminal().Close)
	if tool.Kind() != pane.KindTerminal || tool.TabCount() > 1 {
		t.Fatalf("tool pane must not become a tab host, got kind=%v tabs=%d", tool.Kind(), tool.TabCount())
	}
	if !leafSet(m)[shellKey] {
		t.Fatal("surplus shell must keep its own pane in the implicit flexible region")
	}
}

func TestApplyLayoutRestartsToolTabsInFreshEditor(t *testing.T) {
	withTools(t, sleepTool("watcher"))
	m := sized(t, 120, 40) // explorer + one editor
	// Hand-built layout: explorer + live editor slot + a fresh editor slot
	// whose snapshot hosted a tool tab next to files (#1277 mixed host).
	tree := &layout.Split{Orient: layout.Horizontal, Ratio: 0.25,
		A: &layout.Leaf{Pane: "explorer"},
		B: &layout.Split{Orient: layout.Vertical, Ratio: 0.5,
			A: &layout.Leaf{Pane: "editor"}, B: &layout.Leaf{Pane: "editor:2"}}}
	data, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"mix": {
		Tree: data,
		Panes: map[string]paneIdentity{
			"explorer": {Kind: "explorer"},
			"editor":   {Kind: "editor"},
			"editor:2": {Kind: "editor", Tools: []string{"watcher"}},
		},
	}}})

	m = step(m, ApplyLayoutMsg{Name: "mix"})

	var hosted int
	for key := range leafSet(m) {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			if tt := inst.TabTerminal(i); tt != nil && tt.Tool() == "watcher" {
				hosted++
				t.Cleanup(tt.Close)
			}
		}
	}
	if hosted != 1 {
		t.Fatalf("fresh editor slot must restart the hosted tool tab, got %d", hosted)
	}
}

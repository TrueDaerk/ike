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

package app

import (
	"testing"

	"ike/internal/config"
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

// cleanupLeafTerminals defers closeLeafTerminals so sessions end even when an
// assertion fails first.
func cleanupLeafTerminals(t *testing.T, m Model) {
	t.Helper()
	t.Cleanup(func() { closeLeafTerminals(m) })
}

// TestRestoreDefaultLayoutAdoptsTabHostedTool pins the #2124 duplicate: a tool
// live as a tab host (the shape a home-dock or slot open produces) must re-slot
// into the layout's dedicated "tool" slot — one instance, same session — not
// restart there while the live one grafts into the flexible region.
func TestRestoreDefaultLayoutAdoptsTabHostedTool(t *testing.T) {
	withTools(t, sleepTool("alpha"))
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "alpha"})
	host := m.toolPane("alpha")
	if !host.ConvertToTabHost() {
		t.Fatal("tool pane must convert to a tab host")
	}
	sessKey := host.TabTerminal(0).SessionKey()
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": {
		Tree: encodeTree(t, &layout.Split{Orient: layout.Horizontal, Ratio: 0.25,
			A: &layout.Leaf{Pane: "explorer"},
			B: &layout.Split{Orient: layout.Vertical, Ratio: 0.7,
				A: &layout.Leaf{Pane: "editor"}, B: &layout.Leaf{Pane: "terminal"}}}),
		Panes: map[string]paneIdentity{
			"explorer": {Kind: "explorer"},
			"editor":   {Kind: "editor"},
			"terminal": {Kind: "tool", Tool: "alpha"},
		},
	}}, Default: "dev"})

	m = step(m, RestoreDefaultLayoutMsg{})
	cleanupLeafTerminals(t, m)

	locs := m.toolLocations("alpha")
	if len(locs) != 1 {
		t.Fatalf("restore must reuse the live tool, got %d instances", len(locs))
	}
	inst := m.activeWS().Panes.Get(locs[0].key)
	if locs[0].tab >= 0 || inst.Kind() != pane.KindTerminal {
		t.Fatalf("adopted tool must fill the dedicated slot, got tab=%d kind=%v", locs[0].tab, inst.Kind())
	}
	if inst.Terminal().SessionKey() != sessKey {
		t.Fatal("the slot must host the original live session, not a restart")
	}
}

// TestApplyLayoutExtractsToolFromMultiToolHost: a tool tabbed next to another
// tool moves out into the layout's dedicated slot instead of duplicating; the
// remaining tab keeps its session in the leftover host.
func TestApplyLayoutExtractsToolFromMultiToolHost(t *testing.T) {
	withTools(t,
		config.ToolEntry{Name: "alpha", Command: "sleep", Args: []string{"60"}, Placement: "bottom"},
		config.ToolEntry{Name: "beta", Command: "sleep", Args: []string{"60"}, Placement: "bottom"},
	)
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "alpha"})
	m = step(m, ToolOpenMsg{Name: "beta"}) // tab-joins alpha's dock host
	locs := m.toolLocations("alpha")
	if len(locs) != 1 || locs[0].tab < 0 {
		t.Fatalf("setup: alpha must be tab-hosted, got %+v", locs)
	}
	hostKey := locs[0].key
	if bl := m.toolLocations("beta"); len(bl) != 1 || bl[0].key != hostKey {
		t.Fatalf("setup: beta must share alpha's host, got %+v", bl)
	}
	aSess := m.activeWS().Panes.Get(hostKey).TabTerminal(locs[0].tab).SessionKey()
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": {
		Tree: encodeTree(t, &layout.Split{Orient: layout.Horizontal, Ratio: 0.25,
			A: &layout.Leaf{Pane: "explorer"},
			B: &layout.Split{Orient: layout.Vertical, Ratio: 0.7,
				A: &layout.Leaf{Pane: "editor"}, B: &layout.Leaf{Pane: "terminal"}}}),
		Panes: map[string]paneIdentity{
			"explorer": {Kind: "explorer"},
			"editor":   {Kind: "editor"},
			"terminal": {Kind: "tool", Tool: "alpha"},
		},
	}}})

	m = step(m, ApplyLayoutMsg{Name: "dev"})
	cleanupLeafTerminals(t, m)

	al := m.toolLocations("alpha")
	if len(al) != 1 {
		t.Fatalf("alpha must exist exactly once, got %d", len(al))
	}
	if al[0].tab >= 0 {
		t.Fatal("alpha must move into the dedicated slot, not stay a tab")
	}
	inst := m.activeWS().Panes.Get(al[0].key)
	if inst.Terminal().SessionKey() != aSess {
		t.Fatal("the slot must host alpha's original session")
	}
	if bl := m.toolLocations("beta"); len(bl) != 1 {
		t.Fatalf("beta must survive exactly once, got %d", len(bl))
	}
}

// TestApplyLayoutAdoptsDedicatedPaneIntoToolsHost: the inverse shape — the
// layout hosts the tool as a tab ("tools" pane) while it is live as a
// dedicated pane; the session moves into the host instead of restarting.
func TestApplyLayoutAdoptsDedicatedPaneIntoToolsHost(t *testing.T) {
	withTools(t, sleepTool("alpha"))
	m := sized(t, 120, 40)
	m = step(m, ToolOpenMsg{Name: "alpha"}) // dedicated adaptive pane
	sessKey := m.toolPane("alpha").Terminal().SessionKey()
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"dev": {
		Tree: encodeTree(t, &layout.Split{Orient: layout.Horizontal, Ratio: 0.25,
			A: &layout.Leaf{Pane: "explorer"},
			B: &layout.Split{Orient: layout.Vertical, Ratio: 0.7,
				A: &layout.Leaf{Pane: "editor"}, B: &layout.Leaf{Pane: "editor:2"}}}),
		Panes: map[string]paneIdentity{
			"explorer": {Kind: "explorer"},
			"editor":   {Kind: "editor"},
			"editor:2": {Kind: "tools", Tools: []string{"alpha"}},
		},
	}}})

	m = step(m, ApplyLayoutMsg{Name: "dev"})
	cleanupLeafTerminals(t, m)

	locs := m.toolLocations("alpha")
	if len(locs) != 1 {
		t.Fatalf("alpha must exist exactly once, got %d", len(locs))
	}
	if locs[0].tab < 0 {
		t.Fatal("alpha must re-slot as a tab of the layout's tools host")
	}
	inst := m.activeWS().Panes.Get(locs[0].key)
	if inst.Kind() != pane.KindEditor {
		t.Fatalf("tools slot must be a tab host, got %v", inst.Kind())
	}
	if inst.TabTerminal(locs[0].tab).SessionKey() != sessKey {
		t.Fatal("the host must adopt the original live session, not a restart")
	}
}

// encodeTree encodes a hand-built tree, failing the test on error.
func encodeTree(t *testing.T, tree layout.Node) []byte {
	t.Helper()
	data, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	return data
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

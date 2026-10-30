package app

import (
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/run"
)

// editor_anchor_test.go pins #3109: with no live editor, a file open and the
// tool-pane opens land in the flexible region — the area the reference
// layout reserves for the editor — even when a tool window (the Issues
// window) stands in that slot, instead of carving the editor out of the
// explorer's own column or docking Run/Debug beside the bottom-strip tool
// the focus sits in.

// issue3109Model builds the live tree of the issue — `E | I` over
// `C | L | H` (explorer, Issues window, two custom tools, HTTP response) —
// under a designated default layout `E | editor` over `C | L | H`. It
// returns the model and the keys of the two custom tool panes.
func issue3109Model(t *testing.T) (Model, string, string) {
	t.Helper()
	withTools(t, sleepTool("claude"), sleepTool("lazygit"))
	m := sized(t, 120, 40)

	saved := &layout.Split{Orient: layout.Vertical, Ratio: 2.0 / 3,
		A: &layout.Split{Orient: layout.Horizontal, Ratio: 0.2,
			A: &layout.Leaf{Pane: "explorer"}, B: &layout.Leaf{Pane: "editor"}},
		B: &layout.Split{Orient: layout.Horizontal, Ratio: 1.0 / 3,
			A: &layout.Leaf{Pane: "tool:c"},
			B: &layout.Split{Orient: layout.Horizontal, Ratio: 0.5,
				A: &layout.Leaf{Pane: "tool:l"}, B: &layout.Leaf{Pane: "http"}}}}
	data, err := layout.Encode(saved)
	if err != nil {
		t.Fatal(err)
	}
	saveUserLayouts(savedLayouts{Default: "dev", Layouts: map[string]persistedLayout{"dev": {
		Tree: data,
		Panes: map[string]paneIdentity{
			"explorer": {Kind: "explorer"},
			"editor":   {Kind: "editor"},
			"tool:c":   {Kind: "tool", Tool: "claude"},
			"tool:l":   {Kind: "tool", Tool: "lazygit"},
			"http":     {Kind: "http"},
		},
	}}})

	ws := m.activeWS()
	// Drop the default editor leaf: only the explorer stays, then the live
	// tree is built by hand around the tool panes.
	for _, key := range layout.Leaves(ws.Tree) {
		if key != pane.ExplorerKey {
			ws.Panes.Close(key)
		}
	}
	c := m.addToolPane(m.customToolSpawn(sleepTool("claude")))
	l := m.addToolPane(m.customToolSpawn(sleepTool("lazygit")))
	issues := ws.Panes.AddIssues()
	httpKey := ws.Panes.AddHTTP()
	ws.Tree = &layout.Split{Orient: layout.Vertical, Ratio: 2.0 / 3,
		A: &layout.Split{Orient: layout.Horizontal, Ratio: 0.2,
			A: &layout.Leaf{Pane: pane.ExplorerKey}, B: &layout.Leaf{Pane: issues}},
		B: &layout.Split{Orient: layout.Horizontal, Ratio: 1.0 / 3,
			A: &layout.Leaf{Pane: c},
			B: &layout.Split{Orient: layout.Horizontal, Ratio: 0.5,
				A: &layout.Leaf{Pane: l}, B: &layout.Leaf{Pane: httpKey}}}}
	m.setFocus(pane.ExplorerKey)
	m.layout()
	if m.fileEditorKey() != "" || m.activeEditorKey() != "" {
		t.Fatal("setup: no editor may be live")
	}
	return m, c, l
}

// countLeaves returns how many leaves of the live tree are backed by a pane
// of the given kind.
func countLeaves(m Model, kind pane.Kind) int {
	n := 0
	for _, key := range layout.Leaves(m.activeWS().Tree) {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == kind {
			n++
		}
	}
	return n
}

// TestOpenFileWithNoEditorLandsInIssuesArea: the editor appears in the `I`
// area (sharing with the Issues pane), the explorer keeps its width and the
// Issues pane is not duplicated.
func TestOpenFileWithNoEditorLandsInIssuesArea(t *testing.T) {
	m, _, _ := issue3109Model(t)
	defer closeLeafTerminals(m)
	explorerW := m.lay.Panes[pane.ExplorerKey].W

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
	sib := sp.B
	if l, ok := sp.A.(*layout.Leaf); ok && l.Pane == newKey {
		sib = sp.B
	} else {
		sib = sp.A
	}
	leaf, ok := sib.(*layout.Leaf)
	if !ok || leaf.Pane != pane.IssuesKey {
		t.Fatalf("the editor must share the Issues area, sibling=%v", sib)
	}
	if got := m.lay.Panes[pane.ExplorerKey].W; got < explorerW-1 || got > explorerW+1 {
		t.Fatalf("explorer width changed: %d → %d", explorerW, got)
	}
	if n := countLeaves(m, pane.KindIssues); n != 1 {
		t.Fatalf("Issues leaves = %d, want 1", n)
	}
	ed, issues := m.lay.Panes[newKey], m.lay.Panes[pane.IssuesKey]
	if ed.W*ed.H <= issues.W*issues.H {
		t.Fatalf("the editor must take the larger share of the slot: editor %v issues %v", ed, issues)
	}
}

// TestOpenFileWithNoEditorAndEmptySlotSplitsAtHopLevel: with the slot's area
// held by the explorer alone (`E` over `C | L | H`), the editor splits the
// explorer at the saved share of the row — the explorer keeps its width.
func TestOpenFileWithNoEditorAndEmptySlotSplitsAtHopLevel(t *testing.T) {
	m, _, _ := issue3109Model(t)
	defer closeLeafTerminals(m)
	m.closePane(pane.IssuesKey)
	m.layout()

	dir := t.TempDir()
	f := writeTemp(t, dir, "x.txt", "hello\n")
	tm, _ := m.openPath(f, false)
	m = tm.(Model)

	newKey := m.editorWithFile(f)
	sp := parentSplit(m.activeWS().Tree, newKey)
	if sp == nil {
		t.Fatal("the new editor must be a split child")
	}
	if l, ok := sp.A.(*layout.Leaf); !ok || l.Pane != pane.ExplorerKey || sp.Orient != layout.Horizontal {
		t.Fatalf("the editor must sit right of the explorer, got %+v", sp)
	}
	// The saved row gives the explorer 0.2 of 120 = 24 cells — a share of the
	// row, not of the explorer's own cell.
	if got := m.lay.Panes[pane.ExplorerKey].W; got < 23 || got > 25 {
		t.Fatalf("explorer width = %d, want the saved 24", got)
	}
}

// TestRunWithNoEditorOpensInFlexibleRegion: Run with the focus on the HTTP
// pane opens in the flexible region (beside the Issues window), not beside
// the HTTP pane in the bottom strip.
func TestRunWithNoEditorOpensInFlexibleRegion(t *testing.T) {
	m, _, _ := issue3109Model(t)
	defer closeLeafTerminals(m)
	m.setFocus(pane.HTTPKey)

	if !m.openRunTool(&run.Config{Name: "sleep"}, []string{"sleep", "60"}, ".", nil) {
		t.Fatal("the Run tool must open")
	}
	key := runPaneKey(t, m)
	sib, _ := neighbour(t, m, key)
	if sib != pane.IssuesKey {
		t.Fatalf("Run must open in the flexible region (beside %q), got beside %q", pane.IssuesKey, sib)
	}
}

// TestDebugWithNoEditorOpensInFlexibleRegion is the Debug counterpart.
func TestDebugWithNoEditorOpensInFlexibleRegion(t *testing.T) {
	m, _, _ := issue3109Model(t)
	defer closeLeafTerminals(m)
	m.setFocus(pane.HTTPKey)

	m.openDebugPanel()
	if !m.activeWS().Panes.Has(pane.DebugKey) {
		t.Fatal("the debug area must open")
	}
	sib, _ := neighbour(t, m, pane.DebugKey)
	if sib != pane.IssuesKey {
		t.Fatalf("Debug must open in the flexible region (beside %q), got beside %q", pane.IssuesKey, sib)
	}
}

// TestRunWithEditorKeepsBottomDock: with an editor live the Run tool's
// bottom home still joins the layout's tool strip under the editor (#2191
// unchanged).
func TestRunWithEditorKeepsBottomDock(t *testing.T) {
	m, _, _ := issue3109Model(t)
	defer closeLeafTerminals(m)
	m.closePane(pane.IssuesKey)
	m.layout()
	dir := t.TempDir()
	f := writeTemp(t, dir, "x.txt", "hello\n")
	tm, _ := m.openPath(f, false)
	m = tm.(Model)
	if m.activeEditorKey() == "" {
		t.Fatal("setup: an editor must be live")
	}
	if !m.openRunTool(&run.Config{Name: "sleep"}, []string{"sleep", "60"}, ".", nil) {
		t.Fatal("the Run tool must open")
	}
	key := runPaneKey(t, m)
	if sib, _ := neighbour(t, m, key); sib != pane.HTTPKey {
		t.Fatalf("with an editor live Run joins the bottom strip beside %q, got %q", pane.HTTPKey, sib)
	}
}

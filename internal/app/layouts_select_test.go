package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/layout"
	"ike/internal/pane"
)

// layouts_select_test.go covers selective saved layouts (#1568): the pane
// selection mini-map before the name prompt, the flex-placeholder snapshot,
// and the apply that spreads unsaved panes over the placeholder region.

// flexSnapshot hand-builds explorer (left) + flex region (right) — the
// canonical "pin the explorer, leave the rest flexible" layout.
func flexSnapshot(t *testing.T) persistedLayout {
	t.Helper()
	tree := &layout.Split{Orient: layout.Horizontal, Ratio: 0.25,
		A: &layout.Leaf{Pane: "explorer"}, B: &layout.Leaf{Pane: flexKey}}
	data, err := layout.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	return persistedLayout{Tree: data, Panes: map[string]paneIdentity{
		"explorer": {Kind: "explorer"},
		flexKey:    {Kind: "flex"},
	}}
}

func TestSaveLayoutOpensSelectionThenPrompt(t *testing.T) {
	m := sized(t, 100, 40)
	m = step(m, SaveLayoutPromptMsg{})
	if !m.layoutSelectOpen() {
		t.Fatal("window.saveLayout must open the pane selection first")
	}
	if m.layoutSavePromptOpen() {
		t.Fatal("name prompt must not be open yet")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.layoutSelectOpen() || !m.layoutSavePromptOpen() {
		t.Fatal("enter must advance from selection to the name prompt")
	}
	m = typeText(m, "full")
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.layoutSavePromptOpen() {
		t.Fatal("prompt must close after save")
	}
	snap, ok := loadUserLayouts().Layouts["full"]
	if !ok {
		t.Fatal("layout full must be saved")
	}
	// Everything selected: a full snapshot, no placeholder.
	for key, id := range snap.Panes {
		if id.Kind == "flex" {
			t.Fatalf("full selection must not store a placeholder, got %q", key)
		}
	}
}

func TestSaveLayoutSelectionTogglesAndGuardsEmpty(t *testing.T) {
	m := sized(t, 100, 40) // explorer + editor
	m = step(m, SaveLayoutPromptMsg{})
	// Deselect everything: enter must refuse.
	m = step(m, tea.KeyPressMsg{Text: "n", Code: 'n'})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.layoutSelectOpen() || m.layoutSelect.err == "" {
		t.Fatal("an empty selection must be refused with a hint")
	}
	// Re-select all, deselect the focused editor only.
	m = step(m, tea.KeyPressMsg{Text: "a", Code: 'a'})
	if !m.layoutSelect.sel[m.layoutSelect.focus] {
		t.Fatal("a must select every pane")
	}
	focused := m.layoutSelect.focus
	deselectedExplorer := focused == pane.ExplorerKey
	m = step(m, tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	if m.layoutSelect.sel[focused] {
		t.Fatal("space must deselect the highlighted pane")
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.layoutSavePromptOpen() {
		t.Fatal("a partial selection must reach the name prompt")
	}
	m = typeText(m, "part")
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	snap, ok := loadUserLayouts().Layouts["part"]
	if !ok {
		t.Fatal("layout part must be saved")
	}
	flex := 0
	for _, id := range snap.Panes {
		if id.Kind == "flex" {
			flex++
		}
	}
	if flex != 1 {
		t.Fatalf("partial selection must store exactly one placeholder, got %d", flex)
	}
	kept := "explorer"
	if deselectedExplorer {
		kept = "editor"
	}
	if len(snap.Panes) != 2 || snap.Panes[kept].Kind != kept {
		t.Fatalf("the selected %s must stay in the snapshot, got %+v", kept, snap.Panes)
	}
}

func TestSaveLayoutSelectionEscCancels(t *testing.T) {
	m := sized(t, 100, 40)
	m = step(m, SaveLayoutPromptMsg{})
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.layoutSelectOpen() || m.layoutSavePromptOpen() {
		t.Fatal("esc must cancel the whole save flow")
	}
	if len(loadUserLayouts().Layouts) != 0 {
		t.Fatal("nothing must be saved on cancel")
	}
}

func TestLayoutSelectClickTogglesPane(t *testing.T) {
	m := sized(t, 100, 40)
	m = step(m, SaveLayoutPromptMsg{})
	if !m.layoutSelectOpen() {
		t.Fatal("selection must be open")
	}
	// Render once so the click maps against the on-screen geometry.
	_ = m.layoutSelectBody(80)
	ls := m.layoutSelect
	lay := layout.Compute(m.activeWS().Tree, layout.Rect{W: ls.mapW, H: ls.mapH})
	r := lay.Panes[pane.ExplorerKey]
	cx, cy := r.X+r.W/2, ls.canvasTop+r.Y+r.H/2
	m.layoutSelectClick(cx, cy)
	if ls.sel[pane.ExplorerKey] {
		t.Fatal("click must deselect the explorer cell")
	}
	if ls.focus != pane.ExplorerKey {
		t.Fatal("click must focus the hit pane")
	}
	m.layoutSelectClick(cx, cy)
	if !ls.sel[pane.ExplorerKey] {
		t.Fatal("second click must re-select")
	}
	// A click outside the canvas is a no-op.
	m.layoutSelectClick(-1, 0)
	m.layoutSelectClick(0, ls.canvasTop+ls.mapH+2)
	if !ls.sel[pane.ExplorerKey] || ls.focus != pane.ExplorerKey {
		t.Fatal("out-of-canvas clicks must change nothing")
	}
}

func TestLayoutSelectBodyMarksSelectionAndFillsWidth(t *testing.T) {
	m := sized(t, 100, 40)
	m = step(m, SaveLayoutPromptMsg{})
	body := m.layoutSelectBody(72)
	if m.layoutSelect.mapW != 72 {
		t.Fatalf("mini-map must fill the width budget, got %d", m.layoutSelect.mapW)
	}
	if !strings.Contains(body, "✓") {
		t.Fatal("selected cells must carry the ✓ marker")
	}
	if !strings.Contains(body, "\x1b[") {
		t.Fatal("selection must be styled (ANSI expected)")
	}
	// Deselect all: no ✓ remains.
	m = step(m, tea.KeyPressMsg{Text: "n", Code: 'n'})
	if strings.Contains(m.layoutSelectBody(72), "✓") {
		t.Fatal("deselected cells must not carry the ✓ marker")
	}
}

func TestLayoutSelectLabelUsesToolName(t *testing.T) {
	m, termKey := openTestTerminal(t)
	if got := m.layoutSelectLabel(termKey); got != "TERMINAL" {
		t.Fatalf("plain shell must label TERMINAL, got %q", got)
	}
	reg := m.activeWS().Panes
	toolKey := reg.AddTool("lazygit", []string{"true"}, ".", nil, m.host.Send)
	t.Cleanup(func() { reg.Get(toolKey).Terminal().Close() })
	if got := m.layoutSelectLabel(toolKey); got != "LAZYGIT" {
		t.Fatalf("tool pane must label with its tool name, got %q", got)
	}
}

func TestSnapshotLayoutSelectedKeepsFlexAtLargestRegion(t *testing.T) {
	m := sized(t, 100, 40)
	// explorer | (editor / editor:N) — deselect both editors: their column
	// collapses into one flex leaf beside the explorer.
	m = step(m, SplitFocusedMsg{Zone: layout.ZoneBottom})
	sel := map[string]bool{}
	for _, key := range layout.Leaves(m.activeWS().Tree) {
		sel[key] = key == pane.ExplorerKey
	}
	snap, ok := snapshotLayoutSelected(m.activeWS().Tree, m.activeWS().Panes, sel)
	if !ok {
		t.Fatal("selective snapshot failed")
	}
	tree, leaves, ok := layout.DecodeTree(snap.Tree)
	if !ok || tree == nil {
		t.Fatal("snapshot tree does not decode")
	}
	if len(leaves) != 2 {
		t.Fatalf("explorer + flex expected, got %v", leaves)
	}
	if snap.Panes[flexKey].Kind != "flex" || snap.Panes["explorer"].Kind != "explorer" {
		t.Fatalf("unexpected identities: %+v", snap.Panes)
	}
}

func TestApplyFlexLayoutSpreadsUnsavedPanes(t *testing.T) {
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
	edKey1 := m.activeWS().Panes.Focused()
	m = step(m, SplitFocusedMsg{Zone: layout.ZoneRight})
	edKey2 := m.activeWS().Panes.Focused()
	m = step(m, explorer.OpenFileMsg{Path: pb})
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"flex": flexSnapshot(t)}})

	m = step(m, ApplyLayoutMsg{Name: "flex"})

	leaves := leafSet(m)
	if !leaves[pane.ExplorerKey] {
		t.Fatal("explorer must fill its pinned slot")
	}
	if !leaves[edKey1] || !leaves[edKey2] {
		t.Fatalf("both editor panes must keep their own leaf in the flex region, got %v", leaves)
	}
	if len(leaves) != 3 {
		t.Fatalf("explorer + two editors expected, got %v", leaves)
	}
	for _, key := range []string{edKey1, edKey2} {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			t.Fatalf("pane %q must survive the apply untouched", key)
		}
	}
	if m.activeWS().Panes.Get(edKey1).TabForPath(pa) < 0 {
		t.Fatal("first editor must keep its file")
	}
	if m.activeWS().Panes.Get(edKey2).TabForPath(pb) < 0 {
		t.Fatal("second editor must keep its file")
	}
}

func TestApplyFlexLayoutWithNothingLeftOverAddsScratch(t *testing.T) {
	m := sized(t, 100, 40) // explorer + one empty editor
	// A flex layout pinning only the explorer: the sole editor is unconsumed
	// and flows into the region; closing it first leaves nothing to graft.
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"flex": flexSnapshot(t)}})
	m = step(m, ApplyLayoutMsg{Name: "flex"})
	leaves := layout.Leaves(m.activeWS().Tree)
	if len(leaves) != 2 {
		t.Fatalf("explorer + flex region expected, got %v", leaves)
	}
	editors := 0
	for _, key := range leaves {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			editors++
		}
	}
	if editors != 1 {
		t.Fatalf("the flex region must hold one editor pane, got %d", editors)
	}
}

func TestRestoreFlexDefaultLayoutAtStartup(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	saveUserLayouts(savedLayouts{Layouts: map[string]persistedLayout{"flex": flexSnapshot(t)}, Default: "flex"})
	m := New() // fresh project: the flex default materializes
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	leaves := layout.Leaves(m.activeWS().Tree)
	if len(leaves) != 2 {
		t.Fatalf("explorer + scratch editor expected, got %v", leaves)
	}
	for _, key := range leaves {
		if key == flexKey {
			t.Fatal("the placeholder must not survive startup materialization")
		}
	}
	editors := 0
	for _, key := range leaves {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			editors++
		}
	}
	if editors != 1 {
		t.Fatalf("flex must materialize as one scratch editor, got %d", editors)
	}
}

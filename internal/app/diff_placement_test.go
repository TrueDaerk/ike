package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// diff_placement_test.go guards #2507: a diff opens as a content tab of the
// pane the user works in — the focused flex pane, else the most recently
// focused one — instead of carving off yet another split. The split stays
// reachable through diff.placement = "split" and as the fallback for a layout
// without any flexible pane.

// diffPair writes two differing files into a temp dir and returns their paths.
func diffPair(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	if err := os.WriteFile(left, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(right, []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return left, right
}

// twoEditorApp builds a model whose layout holds two file-backed editor panes
// side by side, returning it plus both pane keys (left first).
func twoEditorApp(t *testing.T) (Model, string, string) {
	t.Helper()
	dir := t.TempDir()
	one := filepath.Join(dir, "one.txt")
	two := filepath.Join(dir, "two.txt")
	for _, p := range []string{one, two} {
		if err := os.WriteFile(p, []byte("content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newSized()
	tm, _ := m.openPath(one, false)
	m = tm.(Model)
	first := m.activeWS().Panes.Focused()
	m.SplitFocused(layout.ZoneRight)
	tm, _ = m.openPath(two, false)
	m = tm.(Model)
	second := m.activeWS().Panes.Focused()
	if first == second || first == "" || second == "" {
		t.Fatalf("setup: two editor panes expected, got %q and %q", first, second)
	}
	return m, first, second
}

// TestDiffOpensInFocusedEditorPane: the diff becomes a tab of the pane that
// has the keyboard; the other editor pane is untouched and no leaf is added.
func TestDiffOpensInFocusedEditorPane(t *testing.T) {
	left, right := diffPair(t)
	m, other, focused := twoEditorApp(t)
	before := len(layout.Leaves(m.activeWS().Tree))

	m.openDiffPane(left, right)

	if got := len(layout.Leaves(m.activeWS().Tree)); got != before {
		t.Fatalf("the diff split a new pane: leaves %d -> %d", before, got)
	}
	if got := m.activeWS().Panes.Focused(); got != focused {
		t.Fatalf("focus = %q, want the working pane %q", got, focused)
	}
	if c := m.focusedContent(); c == nil || c.Kind() != pane.KindDiff {
		t.Fatal("the focused pane's active tab must be the diff")
	}
	if _, _, ok := m.diffInPane(other); ok {
		t.Fatal("the other editor pane must not have received the diff")
	}
}

// TestDiffFromToolWindowUsesRecentFlexPane: with a tool window focused the
// diff lands in the most recently focused editor pane, never in the tool.
func TestDiffFromToolWindowUsesRecentFlexPane(t *testing.T) {
	left, right := diffPair(t)
	m, _, editorKey := twoEditorApp(t)
	toolKey := m.activeWS().Panes.AddProblems()
	if !m.insertToolPane(toolKey, editorKey, layout.ZoneBottom) {
		t.Fatal("could not insert the tool window")
	}
	m.setFocus(toolKey)
	m.layout()
	before := len(layout.Leaves(m.activeWS().Tree))

	m.openDiffPane(left, right)

	if got := len(layout.Leaves(m.activeWS().Tree)); got != before {
		t.Fatalf("the diff split a new pane: leaves %d -> %d", before, got)
	}
	if got := m.activeWS().Panes.Focused(); got != editorKey {
		t.Fatalf("focus = %q, want the last working editor %q", got, editorKey)
	}
	if _, _, ok := m.diffInPane(toolKey); ok {
		t.Fatal("the tool window must never host the diff")
	}
	if inst := m.activeWS().Panes.Get(toolKey); inst == nil || inst.Kind() != pane.KindProblems {
		t.Fatal("the tool window must stay what it was")
	}
}

// TestDiffMRUPrefersLastFlexPaneOverLastEditor: the MRU covers the whole
// flexible region, not just the editor kinds — a viewer pane the user worked
// in last wins over an older editor pane once a tool window takes the keys.
func TestDiffMRUPrefersLastFlexPaneOverLastEditor(t *testing.T) {
	left, right := diffPair(t)
	m, _, editorKey := twoEditorApp(t)

	// A dedicated markdown preview pane beside the editors, focused last.
	previewKey := m.activeWS().Panes.AddMarkdownPreview("r.md")
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, editorKey, previewKey, layout.ZoneRight)
	if !ok {
		t.Fatal("could not split off the preview pane")
	}
	m.activeWS().Tree = tree
	m.setFocus(previewKey)
	m.layout()
	if m.recentEditor == previewKey {
		t.Fatal("precondition: the preview pane is not an editor pane")
	}

	// A tool window takes the keyboard; the diff must go back to the preview.
	toolKey := m.activeWS().Panes.AddProblems()
	if !m.insertToolPane(toolKey, editorKey, layout.ZoneBottom) {
		t.Fatal("could not insert the tool window")
	}
	m.setFocus(toolKey)
	m.layout()

	m.openDiffPane(left, right)

	if got := m.activeWS().Panes.Focused(); got != previewKey {
		t.Fatalf("focus = %q, want the last flex pane %q", got, previewKey)
	}
	if _, _, ok := m.diffInPane(previewKey); !ok {
		t.Fatal("the preview pane must host the diff tab")
	}
}

// TestDiffWithoutFlexPaneFallsBackToSplit: a layout made of the explorer and a
// tool window has no flexible pane, so placement degrades to the old split.
func TestDiffWithoutFlexPaneFallsBackToSplit(t *testing.T) {
	left, right := diffPair(t)
	m := newSized()
	editorKey := m.activeEditorKey()
	toolKey := m.activeWS().Panes.AddProblems()
	if !m.insertToolPane(toolKey, editorKey, layout.ZoneBottom) {
		t.Fatal("could not insert the tool window")
	}
	if !m.closeKey(editorKey) {
		t.Fatal("could not close the editor pane")
	}
	m.setFocus(toolKey)
	m.layout()
	if _, ok := m.diffTabTarget(); ok {
		t.Fatal("precondition: this layout has no flexible pane")
	}
	before := len(layout.Leaves(m.activeWS().Tree))

	m.openDiffPane(left, right)

	if got := len(layout.Leaves(m.activeWS().Tree)); got != before+1 {
		t.Fatalf("the fallback must split: leaves %d -> %d", before, got)
	}
	if inst := m.activeWS().Panes.FocusedInstance(); inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatal("the split diff pane must take focus")
	}
}

// TestDiffRetargetsOnlyTheTargetPane: single-window reuse (#513) is per pane
// since #2507 — a second diff from another pane opens its own tab there and
// leaves the first pane's diff alone.
func TestDiffRetargetsOnlyTheTargetPane(t *testing.T) {
	left, right := diffPair(t)
	other := filepath.Join(t.TempDir(), "o.txt")
	if err := os.WriteFile(other, []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, first, second := twoEditorApp(t)

	m.setFocus(first)
	m.openDiffPane(left, right)
	m.setFocus(second)
	m.openDiffPane(left, other)

	if n := countDiffViewers(m); n != 2 {
		t.Fatalf("each pane should hold its own diff (diffs=%d)", n)
	}
	inst, _, ok := m.diffInPane(first)
	if !ok || inst.Diff().RightPath() != right {
		t.Fatal("the first pane's diff must keep its pair")
	}
	inst, _, ok = m.diffInPane(second)
	if !ok || inst.Diff().RightPath() != other {
		t.Fatal("the second pane's diff must show the new pair")
	}

	// A third diff from the second pane retargets that pane's tab only.
	m.openDiffPane(right, left)
	if n := countDiffViewers(m); n != 2 {
		t.Fatalf("the retarget must not open a third diff (diffs=%d)", n)
	}
	inst, _, _ = m.diffInPane(second)
	if inst.Diff().RightPath() != left {
		t.Fatalf("retarget right = %q, want %q", inst.Diff().RightPath(), left)
	}
}

// TestDiffPlacementSplitKeepsOldBehaviour: diff.placement = "split" restores
// the workspace-wide single slot and the split-beside-the-editor placement.
func TestDiffPlacementSplitKeepsOldBehaviour(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	left, right := diffPair(t)
	m := NewWith(registry.New(), host.MapConfig{"diff.placement": "split"})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	f := filepath.Join(t.TempDir(), "open.txt")
	if err := os.WriteFile(f, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(f, false)
	m = tm.(Model)
	editorKey := m.activeWS().Panes.Focused()
	before := len(layout.Leaves(m.activeWS().Tree))

	m.openDiffPane(left, right)
	diffKey := m.activeWS().Panes.Focused()
	if got := len(layout.Leaves(m.activeWS().Tree)); got != before+1 {
		t.Fatalf("split placement must split: leaves %d -> %d", before, got)
	}
	if inst := m.activeWS().Panes.Get(diffKey); inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatal("the diff must be its own pane")
	}

	// The one slot is workspace-wide again: a diff opened from the editor
	// pane retargets the diff pane instead of tabbing into the editor.
	m.setFocus(editorKey)
	m.openDiffPane(right, left)
	if n := countDiffViewers(m); n != 1 {
		t.Fatalf("split placement keeps one diff window (diffs=%d)", n)
	}
	if got := m.activeWS().Panes.Focused(); got != diffKey {
		t.Fatalf("focus = %q, want the retargeted diff pane %q", got, diffKey)
	}
}

// TestDiffPlacementEveryEntryPoint: every diff-open goes through the shared
// helper, so each lands as a tab of the focused editor pane without splitting.
func TestDiffPlacementEveryEntryPoint(t *testing.T) {
	left, right := diffPair(t)
	cases := map[string]func(m *Model){
		"diff.files":   func(m *Model) { m.openDiffPane(left, right) },
		"vcs.diffHead": func(m *Model) { m.openDiffHeadPane(right, "old\n") },
		"localHistory": func(m *Model) { m.openDiffTexts(right, "r.txt @ then", "r.txt", "old\n", "new\n", true) },
		"clipboard":    func(m *Model) { m.openClipboardDiffPane("r.txt", right, "clip\n", "new\n", true) },
		"http":         func(m *Model) { m.openDiffTexts("", "run 1", "run 2", "a\n", "b\n", false) },
	}
	for name, open := range cases {
		t.Run(name, func(t *testing.T) {
			m, other, focused := twoEditorApp(t)
			before := len(layout.Leaves(m.activeWS().Tree))

			open(&m)

			if got := len(layout.Leaves(m.activeWS().Tree)); got != before {
				t.Fatalf("%s split a new pane: leaves %d -> %d", name, before, got)
			}
			if got := m.activeWS().Panes.Focused(); got != focused {
				t.Fatalf("%s focused %q, want the working pane %q", name, got, focused)
			}
			if c := m.focusedContent(); c == nil || c.Kind() != pane.KindDiff {
				t.Fatalf("%s did not open a diff in the working pane", name)
			}
			if _, _, ok := m.diffInPane(other); ok {
				t.Fatalf("%s leaked into the other pane", name)
			}
		})
	}
}

// TestFlexPaneClassification: the flexible region is the editor area — the
// explorer, the tool windows and a terminal pane are never diff targets.
func TestFlexPaneClassification(t *testing.T) {
	m := newSized()
	reg := m.activeWS().Panes
	cases := []struct {
		name string
		key  string
		want bool
	}{
		{"editor", reg.AddEditor(), true},
		{"diff", reg.AddDiff("l", "r"), true},
		{"preview", reg.AddMarkdownPreview("r.md"), true},
		{"explorer", pane.ExplorerKey, false},
		{"vcs", reg.AddVCS(), false},
		{"problems", reg.AddProblems(), false},
		{"http", reg.AddHTTP(), false},
	}
	for _, c := range cases {
		if got := flexPane(reg.Get(c.key)); got != c.want {
			t.Errorf("flexPane(%s) = %v, want %v", c.name, got, c.want)
		}
	}
	if flexPane(nil) {
		t.Error("flexPane(nil) must be false")
	}
}

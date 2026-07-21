package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// editorLeaves counts editor leaves in the current tree.
func editorLeaves(m Model) int {
	n := 0
	for _, k := range layout.Leaves(m.activeWS().Tree) {
		if k != pane.ExplorerKey {
			n++
		}
	}
	return n
}

// TestSplitFocusedAddsEditor splits the focused leaf and focuses the new pane.
func TestSplitFocusedAddsEditor(t *testing.T) {
	m := sized(t, 100, 40)
	m.cycleFocus() // focus the editor
	before := editorLeaves(m)
	m.SplitFocused(layout.ZoneRight)
	if got := editorLeaves(m); got != before+1 {
		t.Fatalf("editor leaves = %d, want %d", got, before+1)
	}
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindEditor {
		t.Fatal("split should focus the new editor")
	}
	// The new pane is a distinct instance from the original editor.
	if m.activeWS().Panes.Focused() == "editor" {
		t.Fatal("focus should be the freshly split editor, not the original")
	}
}

// TestCloseFocusedCollapses closes a split editor and refocuses a survivor.
func TestCloseFocusedCollapses(t *testing.T) {
	m := sized(t, 100, 40)
	m.cycleFocus()
	m.SplitFocused(layout.ZoneRight)
	newKey := m.activeWS().Panes.Focused()
	m.CloseFocused()
	if m.activeWS().Panes.Has(newKey) {
		t.Fatal("closed editor instance should be gone")
	}
	if editorLeaves(m) != 1 {
		t.Fatalf("want 1 editor leaf after close, got %d", editorLeaves(m))
	}
}

// TestCtrlWClosesFocusedPane verifies ctrl+w closes the focused editor pane and
// is a no-op on the explorer (the singleton survives).
func TestCtrlWClosesFocusedPane(t *testing.T) {
	m := sized(t, 100, 40)
	m.cycleFocus()
	m.SplitFocused(layout.ZoneRight)
	newKey := m.activeWS().Panes.Focused()
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	m = tm.(Model)
	if m.activeWS().Panes.Has(newKey) {
		t.Fatal("ctrl+w should close the focused editor pane")
	}
	// ctrl+w on the explorer is a no-op.
	m.setFocus(pane.ExplorerKey)
	tm, _ = m.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	m = tm.(Model)
	if !m.activeWS().Panes.Has(pane.ExplorerKey) {
		t.Fatal("ctrl+w must never close the explorer")
	}
}

// TestCloseFocusedRefusesExplorer keeps the singleton explorer.
func TestCloseFocusedRefusesExplorer(t *testing.T) {
	m := sized(t, 100, 40)
	// explorer is focused at start
	m.CloseFocused()
	if !m.activeWS().Panes.Has(pane.ExplorerKey) {
		t.Fatal("explorer must never be closed")
	}
}

// TestFocusDirMovesSpatially moves focus to the spatially adjacent pane.
func TestFocusDirMovesSpatially(t *testing.T) {
	m := sized(t, 100, 40)
	// Default layout: explorer left, editor right.
	m.setFocus(pane.ExplorerKey)
	m.FocusDir(DirRight)
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindEditor {
		t.Fatal("focus-right from explorer should land on the editor")
	}
	m.FocusDir(DirLeft)
	if m.activeWS().Panes.Focused() != pane.ExplorerKey {
		t.Fatal("focus-left should return to the explorer")
	}
}

// TestFocusTargetPrefersAxisOverlap reproduces the screenshot grid: a tall
// explorer on the left, two stacked panes in the middle and right columns, and
// a full-width pane along the bottom. Directional moves must follow geometry,
// not land on the wide bottom pane just because its centre is close.
func TestFocusTargetPrefersAxisOverlap(t *testing.T) {
	// Columns: explorer [0,30), middle [30,60), right [60,100).
	// Rows: top [0,20), bottom [20,38) for the columns; floating spans
	// [0,100) over [38,40).
	panes := map[string]layout.Rect{
		"explorer": {X: 0, Y: 0, W: 30, H: 38},
		"progress": {X: 30, Y: 0, W: 30, H: 20},  // middle-top
		"host":     {X: 30, Y: 20, W: 30, H: 18}, // middle-bottom
		"extend":   {X: 60, Y: 0, W: 40, H: 20},  // right-top
		"colors":   {X: 60, Y: 20, W: 40, H: 18}, // right-bottom
		"floating": {X: 0, Y: 38, W: 100, H: 2},  // full-width bottom band
	}
	cases := []struct {
		from string
		dir  Direction
		want string
	}{
		{"explorer", DirRight, "progress"}, // explorer -> middle-top
		{"progress", DirRight, "extend"},   // not the wide bottom pane
		{"extend", DirDown, "colors"},      // right-top -> right-bottom
		{"colors", DirDown, "floating"},    // right-bottom -> bottom band
		{"progress", DirDown, "host"},      // middle-top -> middle-bottom
		{"extend", DirLeft, "progress"},    // right-top -> middle-top
		{"floating", DirUp, "host"},        // bottom band -> nearest above
	}
	for _, c := range cases {
		if got := focusTarget(panes, c.from, c.dir); got != c.want {
			t.Errorf("focusTarget(%s, %v) = %q, want %q", c.from, c.dir, got, c.want)
		}
	}
}

// TestFocusKeysCtrlArrowDefault verifies the default Ctrl+arrow bindings move
// focus spatially through Update (not just the FocusDir method).
func TestFocusKeysCtrlArrowDefault(t *testing.T) {
	m := sized(t, 100, 40) // explorer left, editor right
	if m.activeWS().Panes.Focused() != pane.ExplorerKey {
		t.Fatal("setup: explorer should start focused")
	}
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	m = tm.(Model)
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindEditor {
		t.Fatal("ctrl+right should move focus to the editor")
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != pane.ExplorerKey {
		t.Fatal("ctrl+left should move focus back to the explorer")
	}
}

// TestFocusKeysConfigurable verifies keymap.bindings.focus_* overrides the
// default key, and that an empty override disables a direction.
func TestFocusKeysConfigurable(t *testing.T) {
	keys := focusKeys(host.MapConfig{
		"keymap.bindings.focus_right": "alt+l",
		"keymap.bindings.focus_left":  "",
	})
	if dir, ok := keys["alt+l"]; !ok || dir != DirRight {
		t.Fatalf("focus_right override not applied: %v", keys)
	}
	if _, ok := keys["ctrl+right"]; ok {
		t.Fatal("overridden direction should drop its default key")
	}
	if _, ok := keys["ctrl+left"]; ok {
		t.Fatal("empty override should disable that direction")
	}
	// Untouched directions keep their Ctrl+arrow defaults.
	if keys["ctrl+up"] != DirUp || keys["ctrl+down"] != DirDown {
		t.Fatalf("default vertical bindings lost: %v", keys)
	}
}

// TestOpenInNewPaneSplits verifies the NewPane open target spawns a fresh editor
// rather than replacing the active one.
func TestOpenInNewPaneSplits(t *testing.T) {
	m := sized(t, 100, 40)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	os.WriteFile(a, []byte("aaa\n"), 0o644)
	os.WriteFile(b, []byte("bbb\n"), 0o644)

	tm, _ := m.Update(explorer.OpenFileMsg{Path: a})
	m = tm.(Model)
	if editorLeaves(m) != 1 {
		t.Fatalf("replace open should not add a pane, leaves=%d", editorLeaves(m))
	}
	tm, _ = m.Update(explorer.OpenFileMsg{Path: b, NewPane: true})
	m = tm.(Model)
	if editorLeaves(m) != 2 {
		t.Fatalf("new-pane open should add an editor, leaves=%d", editorLeaves(m))
	}
	// Both files are open in distinct editors.
	if m.editorWithFile(a) == "" || m.editorWithFile(b) == "" {
		t.Fatal("both files should be open in their own editors")
	}
	if m.editorWithFile(a) == m.editorWithFile(b) {
		t.Fatal("the two files should live in different editor panes")
	}
}

// TestMultiEditorPersistAndRestore round-trips a two-editor layout: both files
// reopen in their saved panes.
func TestMultiEditorPersistAndRestore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	proj := t.TempDir()
	t.Chdir(proj)
	a := filepath.Join(proj, "a.txt")
	b := filepath.Join(proj, "b.txt")
	os.WriteFile(a, []byte("aaa\n"), 0o644)
	os.WriteFile(b, []byte("bbb\n"), 0o644)

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	out, _ = m.Update(explorer.OpenFileMsg{Path: a})
	m = out.(Model)
	out, _ = m.Update(explorer.OpenFileMsg{Path: b, NewPane: true})
	m = out.(Model)
	m.quit()

	m2 := NewWith(registry.New(), host.MapConfig{})
	out2, _ := m2.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m2 = out2.(Model)
	if editorLeaves(m2) != 2 {
		t.Fatalf("restored editor leaves = %d, want 2", editorLeaves(m2))
	}
	if m2.editorWithFile(a) == "" || m2.editorWithFile(b) == "" {
		t.Fatal("both files should restore into editors")
	}
}

// TestRestoreMissingFileKeepsEmptyEditor verifies a saved editor whose file is
// gone restores as an empty editor at that leaf (the split is preserved).
func TestRestoreMissingFileKeepsEmptyEditor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	proj := t.TempDir()
	t.Chdir(proj)
	a := filepath.Join(proj, "a.txt")
	gone := filepath.Join(proj, "gone.txt")
	os.WriteFile(a, []byte("aaa\n"), 0o644)
	os.WriteFile(gone, []byte("x\n"), 0o644)

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	out, _ = m.Update(explorer.OpenFileMsg{Path: a})
	m = out.(Model)
	out, _ = m.Update(explorer.OpenFileMsg{Path: gone, NewPane: true})
	m = out.(Model)
	m.quit()
	os.Remove(gone) // the file disappears between sessions

	m2 := NewWith(registry.New(), host.MapConfig{})
	out2, _ := m2.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m2 = out2.(Model)
	if editorLeaves(m2) != 2 {
		t.Fatalf("split should be preserved, leaves=%d", editorLeaves(m2))
	}
	if m2.editorWithFile(a) == "" {
		t.Fatal("the surviving file should still restore")
	}
}

// TestViewNeverOverflowsRect guards the tiling invariant: however many panes are
// split (down to single-cell-wide columns whose titles would otherwise wrap),
// View must render exactly height rows of exactly width columns, so nothing is
// pushed off screen and mouse hit-testing stays aligned with m.lay.
func TestViewNeverOverflowsRect(t *testing.T) {
	for _, dim := range [][2]int{{120, 40}, {100, 30}, {80, 25}, {60, 41}} {
		w, h := dim[0], dim[1]
		m := sized(t, w, h)
		m.cycleFocus() // focus the editor
		// Many splits in mixed zones drive some columns to a few cells wide.
		zones := []layout.Zone{layout.ZoneRight, layout.ZoneBottom, layout.ZoneRight, layout.ZoneTop, layout.ZoneRight, layout.ZoneBottom}
		for _, z := range zones {
			m.SplitFocused(z)
		}
		lines := strings.Split(m.render(), "\n")
		if len(lines) != h {
			t.Fatalf("%dx%d: View height = %d, want %d", w, h, len(lines), h)
		}
	}
}

// TestOpenInNewPaneFromExplorerLandsInEditorArea verifies the new pane splits the
// editor area (explorer stays the leftmost full-height leaf), not the explorer.
func TestOpenInNewPaneFromExplorerLandsInEditorArea(t *testing.T) {
	m := sized(t, 120, 40)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	os.WriteFile(a, []byte("aaa\n"), 0o644)
	os.WriteFile(b, []byte("bbb\n"), 0o644)
	out, _ := m.Update(explorer.OpenFileMsg{Path: a})
	m = out.(Model)
	m.setFocus(pane.ExplorerKey) // back to the explorer, then open-in-new-pane
	out, _ = m.Update(explorer.OpenFileMsg{Path: b, NewPane: true})
	m = out.(Model)
	leaves := layout.Leaves(m.activeWS().Tree)
	if leaves[0] != pane.ExplorerKey {
		t.Fatalf("explorer should stay the leftmost leaf, leaves=%v", leaves)
	}
	// The explorer pane keeps its full body height (it was not split).
	if r := m.lay.Panes[pane.ExplorerKey]; r.H != m.bodyRect().H {
		t.Fatalf("explorer height shrank to %d, want %d (it must not be split)", r.H, m.bodyRect().H)
	}
}

// TestClickAlignedAfterSplit guards click/hover coordinate mapping when the
// explorer no longer sits at the top-left: a vertical split above it gives it a
// non-zero rect.Y, and hover must still resolve the right row through the rect.
func TestClickAlignedAfterSplit(t *testing.T) {
	m := sized(t, 100, 40)
	// Split a new editor ABOVE the explorer, so the explorer moves down.
	m.setFocus(pane.ExplorerKey)
	m.SplitFocused(layout.ZoneTop)
	m.layout()
	r := m.lay.Panes[pane.ExplorerKey]
	if r.Y == 0 {
		t.Fatal("setup: explorer should be pushed below the new split")
	}
	// Hover the third content row of the (relocated) explorer pane.
	wantRow := 2
	hover := tea.MouseMotionMsg{
		X: r.X + paneContentX, Y: r.Y + paneContentY + wantRow,
		Button: tea.MouseNone,
	}
	m = step(m, hover)
	if got := m.explorer().HoverRow(); got != wantRow {
		t.Fatalf("hover row = %d, want %d (coordinate mapping drifted after split)", got, wantRow)
	}
}

// TestMouseSelfEdgeSpawnsSplit verifies dragging a pane title to its own edge
// spawns a new editor split there.
func TestMouseSelfEdgeSpawnsSplit(t *testing.T) {
	m := sized(t, 100, 40)
	before := editorLeaves(m)
	ed := m.lay.Panes[ctxEditor]
	// Grab the editor's title bar, drop on its own right edge — one cell in
	// from the workspace border, which now docks instead (#811).
	m = step(m, press(ed.X+2, ed.Y))
	m = step(m, release(ed.X+ed.W-2, ed.Y+ed.H/2))
	if got := editorLeaves(m); got != before+1 {
		t.Fatalf("self-edge drop should spawn a split, leaves=%d want %d", got, before+1)
	}
}

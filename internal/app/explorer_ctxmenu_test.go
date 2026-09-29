package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// explorerRect finds the explorer pane's rect.
func explorerRect(t *testing.T, m Model) layout.Rect {
	t.Helper()
	r, ok := m.lay.Panes[pane.ExplorerKey]
	if !ok {
		t.Fatal("no explorer rect")
	}
	return r
}

// TestRightClickOpensExplorerContextMenu guards #1040: a right-click on the
// tree opens the node context menu at the pointer; a left press outside
// dismisses it without leaking.
func TestRightClickOpensExplorerContextMenu(t *testing.T) {
	ran := false
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Commands: []plugin.Command{{
		ID: "explorer.refresh", Title: "Refresh",
		Run: func(h host.API) tea.Cmd { ran = true; return nil },
	}}}})
	m := sizedWith(t, reg, 100, 40)
	r := explorerRect(t, m)
	x, y := r.X+paneContentX+3, r.Y+paneContentY
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
	if !m.ctxMenu.IsOpen() {
		t.Fatal("right-click on the explorer must open the context menu")
	}
	// Find the Refresh row (the only runnable entry in this registry).
	px, py := m.ctxMenu.Pos()
	row := -1
	for i, it := range explorerContextItems() {
		if it.Command == "explorer.refresh" {
			row = i
		}
	}
	out, cmd := m.Update(tea.MouseClickMsg{X: px + 1, Y: py + 1 + row, Button: tea.MouseLeft})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("clicking the enabled entry must dispatch")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	if !ran {
		t.Fatal("explorer.refresh must run")
	}
	if m.ctxMenu.IsOpen() {
		t.Fatal("invoking must close the menu")
	}
}

// TestAltEnterOpensExplorerContextMenu guards #2805: alt+enter on the tree
// opens the node menu anchored below the cursor row, esc closes it, and a
// multi-selection survives so the menu's bulk actions see it.
func TestAltEnterOpensExplorerContextMenu(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")
	writeFile(t, a, "a")
	writeFile(t, b, "b")
	m := packTestModel(t, root)
	m = selectEntry(t, m, a)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if !m.ctxMenu.IsOpen() {
		t.Fatal("alt+enter on the explorer must open the context menu")
	}
	r := explorerRect(t, m)
	_, row, _ := m.explorer().ContextRow()
	if _, y := m.ctxMenu.Pos(); y != r.Y+paneContentY+row+1 {
		t.Errorf("menu y = %d, want just below the cursor row (%d)", y, r.Y+paneContentY+row+1)
	}
	if !menuCommands(m.explorerMenuItems())["explorer.compressGzip"] {
		t.Error("a plain file's menu must offer Compress (gzip)")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.ctxMenu.IsOpen() {
		t.Fatal("esc must close the menu")
	}

	// Mark both files: the menu keeps the selection and offers the zip.
	m = runMsg(m, explorer.ToggleMarkMsg{})
	m = selectEntry(t, m, b)
	m = runMsg(m, explorer.ToggleMarkMsg{})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if !m.ctxMenu.IsOpen() {
		t.Fatal("alt+enter must open the menu over a multi-selection")
	}
	if got := len(m.explorer().MarkedPaths()); got != 2 {
		t.Fatalf("marks = %d, want the selection kept", got)
	}
	if !menuCommands(m.explorerMenuItems())["explorer.compressZip"] {
		t.Error("a multi-selection's menu must offer Compress (zip)")
	}
	// The entry acts on the whole selection, as after a right-click.
	m.ctxMenu.Close()
	m = runMsg(m, ExplorerCompressZipMsg{})
	if _, err := os.Stat(filepath.Join(root, "archive.zip")); err != nil {
		t.Fatalf("Compress (zip) on the selection: %v", err)
	}
}

// TestAltEnterStaysCodeActionInEditor: the explorer binding is scoped to the
// tree; the editor's alt+enter is still lsp.codeAction (#2805).
func TestAltEnterStaysCodeActionInEditor(t *testing.T) {
	m := newSized()
	if !m.playEditorChord(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}, "lsp.codeAction") {
		t.Fatal("alt+enter must still resolve to lsp.codeAction in the editor")
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if out.(Model).ctxMenu.IsOpen() {
		t.Fatal("alt+enter in the editor must not open the explorer menu")
	}
}

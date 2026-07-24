package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// rightClick right-clicks the editor content and returns the updated model.
func rightClick(m Model, x, y int) Model {
	out, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
	return out.(Model)
}

// TestRightClickOpensEditorContextMenu guards #1020: a right-click on editor
// content opens the context menu at the pointer and moves the caret there.
func TestRightClickOpensEditorContextMenu(t *testing.T) {
	m, r, _ := cmdClickModel(t)
	x, y := r.X+paneContentX+10, r.Y+paneContentY
	m = rightClick(m, x, y)
	if !m.ctxMenu.IsOpen() {
		t.Fatal("right-click on editor content must open the context menu")
	}
	if px, py := m.ctxMenu.Pos(); px != x || py != y {
		t.Fatalf("menu pos=(%d,%d) want anchored at (%d,%d)", px, py, x, y)
	}
}

// TestContextMenuEntryClickRunsCommand guards #1020: left-clicking an enabled
// entry dispatches its registry command through the RunMsg funnel.
func TestContextMenuEntryClickRunsCommand(t *testing.T) {
	m, r, ran := cmdClickModel(t)
	m = rightClick(m, r.X+paneContentX+10, r.Y+paneContentY)
	// Only lsp.definition is registered in this model, so it is the first
	// runnable entry; find its row via hit-testing down the item column.
	px, py := m.ctxMenu.Pos()
	row := -1
	for i, it := range editorContextItems(false) {
		if it.Command == "lsp.definition" {
			row = i
		}
	}
	if row < 0 {
		t.Fatal("setup: lsp.definition missing from the editor context items")
	}
	out, cmd := m.Update(tea.MouseClickMsg{X: px + 1, Y: py + 1 + row, Button: tea.MouseLeft})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("clicking an enabled entry must return the RunMsg command")
	}
	out, _ = m.Update(cmd())
	m = out.(Model)
	if !*ran {
		t.Fatal("the entry's registry command must run")
	}
	if m.ctxMenu.IsOpen() {
		t.Fatal("invoking an entry must close the menu")
	}
}

// TestContextMenuOutsidePressDismisses guards #1020: a press outside the open
// menu dismisses it without leaking to the panes below.
func TestContextMenuOutsidePressDismisses(t *testing.T) {
	m, r, _ := cmdClickModel(t)
	m = rightClick(m, r.X+paneContentX+10, r.Y+paneContentY)
	before := m.activeWS().Panes.Focused()
	m = step(m, press(0, 0)) // far away from the anchored box
	if m.ctxMenu.IsOpen() {
		t.Fatal("a press outside the menu must dismiss it")
	}
	if m.activeWS().Panes.Focused() != before {
		t.Fatal("the dismissing press must not leak to the panes below")
	}
}

// TestContextMenuEscDismisses guards #1020: the open menu owns the keyboard.
func TestContextMenuEscDismisses(t *testing.T) {
	m, r, _ := cmdClickModel(t)
	m = rightClick(m, r.X+paneContentX+10, r.Y+paneContentY)
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.ctxMenu.IsOpen() {
		t.Fatal("esc must dismiss the context menu")
	}
}

// conflictClickModel builds a model with an open file holding one merge
// conflict at lines 0..4 and a plain line 5, returning the editor pane rect.
func conflictClickModel(t *testing.T) (Model, layout.Rect) {
	t.Helper()
	m := sizedWith(t, registry.New(), 100, 40)
	path := filepath.Join(t.TempDir(), "c.txt")
	content := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> b\nplain\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	for key, rect := range m.lay.Panes {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			return m, rect
		}
	}
	t.Fatal("setup: no editor pane rect")
	return m, layout.Rect{}
}

// ctxItemCount counts the open context menu's entry rows via hit-testing.
func ctxItemCount(m Model) int {
	px, py := m.ctxMenu.Pos()
	n := 0
	for y := py + 1; ; y++ {
		if _, ok := m.ctxMenu.ItemAt(px+1, y); !ok {
			break
		}
		n++
	}
	return n
}

// TestContextMenuMergeEntriesInsideConflict guards #1149: right-clicking
// inside a conflict block appends the three accept entries; outside the block
// the menu keeps its static shape.
func TestContextMenuMergeEntriesInsideConflict(t *testing.T) {
	m, r := conflictClickModel(t)
	// Inside the block (line 1, "ours").
	m = rightClick(m, r.X+paneContentX+2, r.Y+paneContentY+1)
	if !m.ctxMenu.IsOpen() {
		t.Fatal("menu did not open")
	}
	if got, want := ctxItemCount(m), len(editorContextItems(true)); got != want {
		t.Fatalf("inside conflict: %d entries, want %d (with merge accepts)", got, want)
	}
	m.ctxMenu.Close()
	// Outside the block (line 5, "plain").
	m = rightClick(m, r.X+paneContentX+2, r.Y+paneContentY+5)
	if got, want := ctxItemCount(m), len(editorContextItems(false)); got != want {
		t.Fatalf("outside conflict: %d entries, want %d (static menu)", got, want)
	}
}

package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
	for i, it := range editorContextItems() {
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

package explorer

// prompt_anchor_test.go covers #1884: the delete confirmation and the rename
// prompt open next to the affected file's row instead of in the pane centre,
// clamped so the box always renders fully inside the pane.

import (
	"fmt"
	"path/filepath"
	"testing"
)

// manyFiles builds a root holding n files named f00.txt, f01.txt, … so tests
// can scroll the explorer and park the cursor near the bottom edge.
func manyFiles(t *testing.T, n int) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < n; i++ {
		mustWrite(t, filepath.Join(root, fmt.Sprintf("f%02d.txt", i)), "x")
	}
	return root
}

// visibleCursorRow is the content-local row the cursor currently occupies.
func visibleCursorRow(m Model) int {
	_, textH, _, _, _ := m.viewport()
	return m.cursor - clamp(m.offset, 0, maxz(len(m.rows)-textH))
}

func TestDeletePromptAnchorsBelowSelectedRow(t *testing.T) {
	m := mounted(t, tree(t), 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a row below the root
	row := visibleCursorRow(m)
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	_, y, _, h, ok := m.promptBoxOrigin()
	if !ok {
		t.Fatal("an active prompt must always have a render origin")
	}
	if y != row+1 {
		t.Fatalf("delete box y = %d, want %d (directly below row %d)", y, row+1, row)
	}
	if y+h > m.height {
		t.Fatalf("box bottom %d overflows pane height %d", y+h, m.height)
	}
}

func TestRenamePromptAnchorsBelowSelectedRow(t *testing.T) {
	m := mounted(t, tree(t), 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j"))
	row := visibleCursorRow(m)
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	_, y, _, h, ok := m.promptBoxOrigin()
	if !ok {
		t.Fatal("an active prompt must always have a render origin")
	}
	if y != row+1 {
		t.Fatalf("rename box y = %d, want %d (directly below row %d)", y, row+1, row)
	}
	if y+h > m.height {
		t.Fatalf("box bottom %d overflows pane height %d", y+h, m.height)
	}
}

// A row near the bottom edge flips the box above the row, so it never renders
// partially off-screen.
func TestPromptAnchorFlipsAboveNearBottom(t *testing.T) {
	m := mounted(t, manyFiles(t, 40), 40, 12)
	m.SetFocused(true)
	for i := 0; i < 30; i++ {
		m, _ = send(m, key("j"))
	}
	row := visibleCursorRow(m)
	_, textH, _, _, _ := m.viewport()
	if row < textH-2 {
		t.Fatalf("test setup: cursor row %d is not near the bottom of %d", row, textH)
	}
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	_, y, _, h, _ := m.promptBoxOrigin()
	if y >= row {
		t.Fatalf("box y = %d, want above the anchored row %d", y, row)
	}
	if y != row-h {
		t.Fatalf("box y = %d, want %d (directly above row %d)", y, row-h, row)
	}
	if y < 0 || y+h > m.height {
		t.Fatalf("box rows %d..%d escape the pane height %d", y, y+h, m.height)
	}
}

// The anchor follows the scrolled viewport: the box attaches to the entry's
// *visible* row, not its index in the tree.
func TestPromptAnchorUsesVisibleRowWhenScrolled(t *testing.T) {
	m := mounted(t, manyFiles(t, 40), 40, 14)
	m.SetFocused(true)
	for i := 0; i < 20; i++ {
		m, _ = send(m, key("j"))
	}
	if m.offset == 0 {
		t.Fatal("test setup: expected a scrolled viewport")
	}
	row := visibleCursorRow(m)
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	_, y, _, h, _ := m.promptBoxOrigin()
	if y != row+1 && y != row-h {
		t.Fatalf("box y = %d, want %d or %d relative to visible row %d",
			y, row+1, row-h, row)
	}
	if y < 0 || y+h > m.height {
		t.Fatalf("box rows %d..%d escape the pane height %d", y, y+h, m.height)
	}
}

// A multi-select delete (#1044) anchors its single confirm box to the cursor
// row, which is inside the selected range.
func TestMultiDeletePromptAnchorsToCursorRow(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, key("j"), key("j"), selKey("J"))
	row := visibleCursorRow(m)
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	if m.prompt == nil || m.prompt.anchor != m.rows[m.cursor].path {
		t.Fatalf("batch delete prompt should anchor to the cursor row, got %#v", m.prompt)
	}
	_, y, _, h, _ := m.promptBoxOrigin()
	if y != row+1 {
		t.Fatalf("batch delete box y = %d, want %d", y, row+1)
	}
	if y < 0 || y+h > m.height {
		t.Fatalf("box rows %d..%d escape the pane height %d", y, y+h, m.height)
	}
}

// A pane too short for the anchored box still clamps it to row 0 rather than
// letting it hang off the top edge.
func TestPromptAnchorClampsInTinyPane(t *testing.T) {
	m := mounted(t, tree(t), 24, 3)
	m.SetFocused(true)
	m, _ = send(m, key("j"))
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	x, y, _, _, ok := m.promptBoxOrigin()
	if !ok {
		t.Fatal("an active prompt must always have a render origin")
	}
	if y != 0 || x < 0 {
		t.Fatalf("origin = (%d,%d), want clamped to the pane's top-left", x, y)
	}
}

// An unanchored prompt (create) keeps the centred placement.
func TestCreatePromptStaysCentred(t *testing.T) {
	m := mounted(t, tree(t), 40, 20)
	m.SetFocused(true)
	m, cmd := m.Update(NewFileMsg{})
	m, _ = pumpScans(m, cmd)
	if m.prompt == nil || m.prompt.anchor != "" {
		t.Fatalf("create prompt should carry no anchor, got %#v", m.prompt)
	}
	_, y, _, h, _ := m.promptBoxOrigin()
	if want := max(0, (m.height-h)/2); y != want {
		t.Fatalf("create box y = %d, want centred %d", y, want)
	}
}

// The anchored box stays clickable: PromptMouseClick derives the input row
// from the same origin, so a click still lands on the typed rune (#373/#1047).
func TestAnchoredRenamePromptStaysClickable(t *testing.T) {
	m := mounted(t, tree(t), 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j"))
	m, cmd := m.Update(RenameMsg{})
	m, _ = pumpScans(m, cmd)
	bx, by, _, _, _ := m.promptBoxOrigin()
	m.PromptMouseClick(bx+2+len(promptInputPrefix)+2, by+2)
	if m.prompt.pos != 2 {
		t.Fatalf("pos after click at col 2 = %d want 2", m.prompt.pos)
	}
}

// A vanished anchor (the watcher dropped the row while the prompt was open)
// falls back to the centred placement instead of an off-screen origin.
func TestPromptAnchorFallsBackWhenRowGone(t *testing.T) {
	m := mounted(t, tree(t), 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"))
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	m.prompt.anchor = filepath.Join("does", "not", "exist")
	if _, ok := m.promptAnchorRow(); ok {
		t.Fatal("a missing anchor row must not resolve")
	}
	_, y, _, h, _ := m.promptBoxOrigin()
	if want := max(0, (m.height-h)/2); y != want {
		t.Fatalf("box y = %d, want centred %d", y, want)
	}
}

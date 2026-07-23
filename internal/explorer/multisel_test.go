package explorer

// multisel_test.go covers the contiguous multi-select (#1044): shift+j/k and
// shift+click extend a range from an anchor, plain motions/clicks collapse
// it, esc clears it, range members take the rowRange highlight kind, and
// Delete acts on the whole selection with one confirm prompt and one undo
// step restoring everything.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// selKey builds the shifted / special keys the multi-select uses.
func selKey(s string) tea.KeyPressMsg {
	switch s {
	case "shift+down":
		return tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}
	case "shift+up":
		return tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
}

// selTree mounts the standard fixture: rows are root(0), sub(1), a.txt(2),
// b.txt(3).
func selTree(t *testing.T) Model {
	t.Helper()
	m := mounted(t, tree(t), 40, 20)
	m.SetFocused(true)
	return m
}

func TestShiftJExtendsAndPlainMotionCollapses(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, key("j"), key("j")) // cursor on a.txt (row 2)
	m, _ = send(m, selKey("J"))        // extend to b.txt
	lo, hi, ok := m.selRange()
	if !ok || lo != 2 || hi != 3 {
		t.Fatalf("selRange = %d..%d ok=%v, want 2..3 active", lo, hi, ok)
	}
	if m.cursor != 3 || m.selAnchor != 2 {
		t.Fatalf("cursor=%d anchor=%d, want 3/2", m.cursor, m.selAnchor)
	}
	// K shrinks the range back toward the anchor.
	m, _ = send(m, selKey("K"))
	if lo, hi, ok = m.selRange(); !ok || lo != 2 || hi != 2 {
		t.Fatalf("after K selRange = %d..%d ok=%v, want 2..2 active", lo, hi, ok)
	}
	// A plain motion collapses the selection entirely.
	m, _ = send(m, key("k"))
	if _, _, ok = m.selRange(); ok {
		t.Fatal("plain k must collapse the selection")
	}
}

func TestShiftArrowsExtendAndEscClears(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, key("j")) // cursor on sub (row 1)
	m, _ = send(m, selKey("shift+down"), selKey("shift+down"))
	if lo, hi, ok := m.selRange(); !ok || lo != 1 || hi != 3 {
		t.Fatalf("selRange = %d..%d ok=%v, want 1..3 active", lo, hi, ok)
	}
	m, _ = send(m, selKey("shift+up"))
	if lo, hi, ok := m.selRange(); !ok || lo != 1 || hi != 2 {
		t.Fatalf("after shift+up selRange = %d..%d ok=%v, want 1..2", lo, hi, ok)
	}
	m, _ = send(m, selKey("esc"))
	if _, _, ok := m.selRange(); ok {
		t.Fatal("esc must clear the selection")
	}
	if m.cursor != 2 {
		t.Fatalf("esc must not move the cursor, got %d", m.cursor)
	}
}

func TestShiftClickExtendsToClickedRow(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, key("j")) // cursor on sub (row 1)
	m.ShiftClick(3, 3)       // row index 3 (b.txt)
	if lo, hi, ok := m.selRange(); !ok || lo != 1 || hi != 3 {
		t.Fatalf("selRange = %d..%d ok=%v, want 1..3 active", lo, hi, ok)
	}
	if m.cursor != 3 {
		t.Fatalf("cursor = %d, want 3", m.cursor)
	}
	// A plain click collapses again.
	m, _ = clickAt(m, 3, 1, 0)
	if _, _, ok := m.selRange(); ok {
		t.Fatal("plain click must collapse the selection")
	}
}

func TestRangeRowKindAndCursorKeepsSelection(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, key("j"))
	m, _ = send(m, selKey("J"), selKey("J")) // range rows 1..3, cursor 3
	if k := m.rowKind(3); k != rowSelected {
		t.Fatalf("cursor row kind = %v, want rowSelected", k)
	}
	for _, i := range []int{1, 2} {
		if k := m.rowKind(i); k != rowRange {
			t.Fatalf("row %d kind = %v, want rowRange", i, k)
		}
	}
	if k := m.rowKind(0); k != rowDir {
		t.Fatalf("row 0 kind = %v, want rowDir", k)
	}
	// Range members outrank a hover sweeping over them.
	m.hover = 2
	if k := m.rowKind(2); k != rowRange {
		t.Fatalf("hovered range row kind = %v, want rowRange", k)
	}
	// Unfocused, the cursor end falls back to the same muted recipe.
	m.SetFocused(false)
	if k := m.rowKind(3); k != rowRange {
		t.Fatalf("unfocused cursor row kind = %v, want rowRange", k)
	}
}

func TestBatchDeleteOneConfirmOneUndo(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"), key("j")) // a.txt
	m, _ = send(m, selKey("J"))        // + b.txt
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	if !m.Prompting() {
		t.Fatal("batch delete must open one confirm prompt")
	}
	if got := m.prompt.title; !strings.Contains(got, "2 entries") {
		t.Fatalf("prompt title = %q, want the entry count", got)
	}
	m, _ = send(m, key("y"))
	if m.Prompting() {
		t.Fatal("one confirm must cover the whole batch")
	}
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, f)); !os.IsNotExist(err) {
			t.Fatalf("%s should be deleted, err=%v", f, err)
		}
	}
	if len(m.ops) != 1 || len(m.ops[0].batch) != 2 {
		t.Fatalf("want ONE batch op of 2 subs, got ops=%d", len(m.ops))
	}
	// One undo restores the whole selection.
	m, cmd = m.Update(UndoMsg{})
	m, _ = pumpScans(m, cmd)
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Fatalf("%s should be restored by a single undo: %v", f, err)
		}
	}
	if len(m.ops) != 0 {
		t.Fatalf("undo stack should be empty, got %d", len(m.ops))
	}
	// One redo re-deletes both, one more undo restores again.
	m, cmd = m.Update(RedoMsg{})
	m, _ = pumpScans(m, cmd)
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, f)); !os.IsNotExist(err) {
			t.Fatalf("%s should be re-deleted by redo, err=%v", f, err)
		}
	}
	m, cmd = m.Update(UndoMsg{})
	pumpScans(m, cmd)
	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Fatalf("%s should be restored again: %v", f, err)
		}
	}
}

func TestBatchDeleteSkipsNestedChildren(t *testing.T) {
	root := tree(t)
	m := mounted(t, root, 40, 20)
	m.SetFocused(true)
	m, _ = send(m, key("j"))     // sub
	m, _ = send(m, key("enter")) // expand sub: rows root, sub, c.txt, a.txt, b.txt
	m, _ = send(m, selKey("J"))  // range sub..c.txt
	targets := m.selTargets()
	if len(targets) != 1 || filepath.Base(targets[0].path) != "sub" {
		t.Fatalf("targets = %v, want just sub (nested child filtered)", targets)
	}
	m, cmd := m.Update(DeleteMsg{})
	m, _ = pumpScans(m, cmd)
	if got := m.prompt.title; !strings.Contains(got, "1 entry") {
		t.Fatalf("prompt title = %q, want a 1-entry count", got)
	}
	m, _ = send(m, key("y"))
	if _, err := os.Stat(filepath.Join(root, "sub")); !os.IsNotExist(err) {
		t.Fatalf("sub should be deleted, err=%v", err)
	}
	m, cmd = m.Update(UndoMsg{})
	pumpScans(m, cmd)
	if _, err := os.Stat(filepath.Join(root, "sub", "c.txt")); err != nil {
		t.Fatalf("sub/c.txt should be restored with its directory: %v", err)
	}
}

func TestSelectionExcludesRootFromDelete(t *testing.T) {
	m := selTree(t)
	// Anchor on the root, extend down over sub: only sub is a target.
	m, _ = send(m, selKey("J"))
	targets := m.selTargets()
	if len(targets) != 1 || filepath.Base(targets[0].path) != "sub" {
		t.Fatalf("targets = %v, want just sub (root excluded)", targets)
	}
}

func TestToggleHiddenAndRefreshCollapseSelection(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, key("j"))
	m, _ = send(m, selKey("J"))
	m, _ = m.Update(ToggleHiddenMsg{})
	if _, _, ok := m.selRange(); ok {
		t.Fatal("toggling hidden must collapse the selection")
	}
	m, _ = send(m, key("j"))
	m, _ = send(m, selKey("J"))
	m, cmd := m.Update(RefreshMsg{})
	m, _ = pumpScans(m, cmd)
	if _, _, ok := m.selRange(); ok {
		t.Fatal("a manual refresh must collapse the selection")
	}
}

func TestContextClickInsideRangeKeepsSelection(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, key("j"))
	m, _ = send(m, selKey("J"), selKey("J")) // range 1..3
	if !m.ContextClick(3, 2) {               // right-click inside the range
		t.Fatal("context click on a row must report true")
	}
	if lo, hi, ok := m.selRange(); !ok || lo != 1 || hi != 3 {
		t.Fatalf("selRange = %d..%d ok=%v, want 1..3 kept", lo, hi, ok)
	}
	if !m.ContextClick(3, 0) { // right-click outside (root row)
		t.Fatal("context click on the root row must report true")
	}
	if _, _, ok := m.selRange(); ok {
		t.Fatal("a context click outside the range must collapse it")
	}
}

func TestRebuildClampsAnchor(t *testing.T) {
	m := selTree(t)
	m, _ = send(m, selKey("J"), selKey("J"), selKey("J")) // anchor 0, cursor 3
	m.selAnchor = 99                                      // simulate a stale anchor past the rows
	m.rebuild()
	if m.selAnchor != len(m.rows)-1 {
		t.Fatalf("anchor = %d, want clamped to %d", m.selAnchor, len(m.rows)-1)
	}
}

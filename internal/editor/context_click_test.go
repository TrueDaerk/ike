package editor

import (
	"testing"

	"ike/internal/editor/buffer"
)

// TestContextClickMovesCaret guards #1020: a right-click outside any selection
// positions the caret like a plain click, without advancing the click streak.
func TestContextClickMovesCaret(t *testing.T) {
	m, _ := loaded(t, "hello\nworld\nthird\n")
	m.ContextClick(2, 1)
	if m.cursor != (buffer.Position{Line: 1, Col: 2}) {
		t.Fatalf("cursor=%v want {1 2}", m.cursor)
	}
	// A double-click right after must still read as a fresh streak: the
	// context click never primed lastClickPos.
	clickClock(&m)
	m.MouseClick(2, 1)
	if m.ModeName() == Visual {
		t.Fatal("first left click after a context click must not select a word")
	}
}

// TestContextClickInsideSelectionKeepsIt guards #1020: right-clicking inside
// the active selection keeps it, so the menu's Cut/Copy act on the selection.
func TestContextClickInsideSelectionKeepsIt(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	clickClock(&m)
	m.MouseClick(7, 0)
	m.MouseClick(7, 0) // double-click selects "world"
	anchor, cursor := m.anchor, m.cursor
	m.ContextClick(8, 0)
	if m.ModeName() != Visual || m.anchor != anchor || m.cursor != cursor {
		t.Fatalf("selection %v..%v mode=%v; want unchanged %v..%v Visual",
			m.anchor, m.cursor, m.ModeName(), anchor, cursor)
	}
}

// TestContextClickOutsideSelectionCollapses guards #1020: right-clicking
// outside the selection collapses it and moves the caret.
func TestContextClickOutsideSelectionCollapses(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	clickClock(&m)
	m.MouseClick(7, 0)
	m.MouseClick(7, 0) // double-click selects "world"
	m.ContextClick(1, 0)
	if m.ModeName() != Normal {
		t.Fatalf("mode=%v want Normal", m.ModeName())
	}
	if m.cursor != (buffer.Position{Line: 0, Col: 1}) {
		t.Fatalf("cursor=%v want {0 1}", m.cursor)
	}
}

// TestContextClickInsideLineSelectionKeepsIt covers the line-wise branch of
// selectionContains: any cell on a selected line counts as inside.
func TestContextClickInsideLineSelectionKeepsIt(t *testing.T) {
	m, _ := loaded(t, "hello world\nsecond\n")
	clickClock(&m)
	for range 3 {
		m.MouseClick(7, 0) // triple-click selects line 0
	}
	m.ContextClick(0, 0)
	if m.ModeName() != VisualLine {
		t.Fatalf("mode=%v want VisualLine", m.ModeName())
	}
}

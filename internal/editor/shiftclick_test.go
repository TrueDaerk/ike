package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
)

// TestPlainClickDropsShiftSelection guards #2608: a selection made with
// Shift+arrows is GUI-style, so a plain left click drops it and places the
// caret on the clicked cell instead of extending the selection.
func TestPlainClickDropsShiftSelection(t *testing.T) {
	m, _ := loaded(t, "hello world\nsecond line\n")
	m = send(m, modKey(tea.KeyRight, tea.ModShift), modKey(tea.KeyRight, tea.ModShift))
	if !m.mode.IsVisual() || !m.shiftSelect {
		t.Fatalf("setup: mode=%v shiftSelect=%v want visual shift-select", m.mode, m.shiftSelect)
	}
	clickClock(&m)
	m.MouseClick(3, 1)
	if m.ModeName() != Normal {
		t.Fatalf("mode=%v want Normal", m.ModeName())
	}
	if m.shiftSelect {
		t.Fatal("shiftSelect still set after a plain click")
	}
	if m.cursor != (buffer.Position{Line: 1, Col: 3}) {
		t.Fatalf("cursor=%v want {1 3}", m.cursor)
	}
}

// TestShiftClickExtendsShiftSelection guards #2608: Shift+click keeps the
// selection and grows it to the clicked cell.
func TestShiftClickExtendsShiftSelection(t *testing.T) {
	m, _ := loaded(t, "hello world\nsecond line\n")
	m = send(m, modKey(tea.KeyRight, tea.ModShift))
	anchor := m.anchor
	clickClock(&m)
	m.ShiftClick(4, 1)
	if !m.mode.IsVisual() {
		t.Fatalf("mode=%v want visual", m.mode)
	}
	if m.anchor != anchor {
		t.Fatalf("anchor=%v want unchanged %v", m.anchor, anchor)
	}
	if m.cursor != (buffer.Position{Line: 1, Col: 4}) {
		t.Fatalf("cursor=%v want {1 4}", m.cursor)
	}
}

// TestShiftClickStartsSelection guards #2608: without an active selection
// Shift+click starts one at the cursor, as a shift-select selection that a
// later plain click drops again.
func TestShiftClickStartsSelection(t *testing.T) {
	m, _ := loaded(t, "hello world\nsecond line\n")
	clickClock(&m)
	m.ShiftClick(6, 0)
	if m.ModeName() != Visual || !m.shiftSelect {
		t.Fatalf("mode=%v shiftSelect=%v want Visual shift-select", m.ModeName(), m.shiftSelect)
	}
	if m.anchor != (buffer.Position{Line: 0, Col: 0}) || m.cursor != (buffer.Position{Line: 0, Col: 6}) {
		t.Fatalf("selection %v..%v want {0 0}..{0 6}", m.anchor, m.cursor)
	}
	m.MouseClick(1, 0)
	if m.ModeName() != Normal {
		t.Fatalf("mode after plain click=%v want Normal", m.ModeName())
	}
}

// TestPlainClickKeepsKeyboardVisualSelection guards #2608: v/V selections keep
// their click-extends semantics.
func TestPlainClickKeepsKeyboardVisualSelection(t *testing.T) {
	m, _ := loaded(t, "hello world\nsecond line\n")
	m = typeKeys(m, "v")
	anchor := m.anchor
	clickClock(&m)
	m.MouseClick(3, 1)
	if m.ModeName() != Visual {
		t.Fatalf("mode=%v want Visual", m.ModeName())
	}
	if m.anchor != anchor || m.cursor != (buffer.Position{Line: 1, Col: 3}) {
		t.Fatalf("selection %v..%v want %v..{1 3}", m.anchor, m.cursor, anchor)
	}
}

// TestShiftClickNoStreakEscalation guards #2608: a Shift+click never counts
// towards the multi-click streak, so a following double-click still selects a
// word rather than escalating to a line.
func TestShiftClickNoStreakEscalation(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	clickClock(&m)
	m.MouseClick(7, 0)
	m.ShiftClick(7, 0)
	m.MouseClick(7, 0)
	m.MouseClick(7, 0)
	if m.ModeName() != Visual {
		t.Fatalf("mode=%v want Visual", m.ModeName())
	}
	if m.anchor != (buffer.Position{Line: 0, Col: 6}) || m.cursor != (buffer.Position{Line: 0, Col: 10}) {
		t.Fatalf("selection %v..%v want the word {0 6}..{0 10}", m.anchor, m.cursor)
	}
}

// TestPlainClickAfterShiftSelectionStillDrags guards #2608: the plain click
// that drops a shift selection still arms a char-wise drag from the click cell.
func TestPlainClickAfterShiftSelectionStillDrags(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	m = send(m, modKey(tea.KeyRight, tea.ModShift))
	clickClock(&m)
	m.MouseClick(2, 0)
	m.MouseDrag(6, 0)
	if m.ModeName() != Visual {
		t.Fatalf("mode=%v want Visual after drag", m.ModeName())
	}
	if m.anchor != (buffer.Position{Line: 0, Col: 2}) || m.cursor != (buffer.Position{Line: 0, Col: 6}) {
		t.Fatalf("drag selection %v..%v want {0 2}..{0 6}", m.anchor, m.cursor)
	}
}

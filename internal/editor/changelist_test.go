package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// changelist_test.go covers the vim change list (#1174): the g;/g, walk over
// recorded edit positions, first-jump semantics, adjacent same-line dedupe,
// pointer reset on new edits, line-shift/clamp behavior, and the boundary
// notices. The walk is cursor motion only — no undo, no nav-history entry.

// TestChangeListFirstJumpGoesToMostRecentEdit: after editing away from the
// cursor, the first g; lands on the most recent edit position.
func TestChangeListFirstJumpGoesToMostRecentEdit(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\nl2\nl3\n")
	m = typeKeys(m, "jjx") // edit on line 2
	m = typeKeys(m, "gg")
	m = typeKeys(m, "g;")
	if l, _ := m.CursorPos(); l != 2 {
		t.Fatalf("g; landed on line %d, want 2", l)
	}
	if !strings.Contains(m.cmdMsg, "change list: 1/1") {
		t.Fatalf("cmdMsg = %q, want a change list: 1/1 notice", m.cmdMsg)
	}
}

// TestChangeListWalkOlderAndNewer: g; walks toward older positions, g, back
// toward newer, with notices at both ends.
func TestChangeListWalkOlderAndNewer(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\nl2\nl3\nl4\n")
	m = typeKeys(m, "x")    // line 0
	m = typeKeys(m, "jjx")  // line 2
	m = typeKeys(m, "jjx")  // line 4
	m = typeKeys(m, "gg")

	m = typeKeys(m, "g;") // newest: line 4
	if l, _ := m.CursorPos(); l != 4 {
		t.Fatalf("1st g; on line %d, want 4", l)
	}
	m = typeKeys(m, "g;") // line 2
	if l, _ := m.CursorPos(); l != 2 {
		t.Fatalf("2nd g; on line %d, want 2", l)
	}
	if !strings.Contains(m.cmdMsg, "change list: 2/3") {
		t.Fatalf("cmdMsg = %q, want change list: 2/3", m.cmdMsg)
	}
	m = typeKeys(m, "g;") // line 0
	if l, _ := m.CursorPos(); l != 0 {
		t.Fatalf("3rd g; on line %d, want 0", l)
	}
	m = typeKeys(m, "g;") // past the oldest: notice, no move
	if l, _ := m.CursorPos(); l != 0 {
		t.Fatalf("g; past the oldest moved to line %d", l)
	}
	if !strings.Contains(m.cmdMsg, "no earlier edit position") {
		t.Fatalf("cmdMsg = %q, want no earlier edit position", m.cmdMsg)
	}

	m = typeKeys(m, "g,") // back toward newer: line 2
	if l, _ := m.CursorPos(); l != 2 {
		t.Fatalf("g, on line %d, want 2", l)
	}
	m = typeKeys(m, "g,") // line 4
	if l, _ := m.CursorPos(); l != 4 {
		t.Fatalf("g, on line %d, want 4", l)
	}
	m = typeKeys(m, "g,") // past the newest: notice, no move
	if l, _ := m.CursorPos(); l != 4 {
		t.Fatalf("g, past the newest moved to line %d", l)
	}
	if !strings.Contains(m.cmdMsg, "no later edit position") {
		t.Fatalf("cmdMsg = %q, want no later edit position", m.cmdMsg)
	}
}

// TestChangeListCount: a count walks several entries at once, clamped at the
// end of the list.
func TestChangeListCount(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\nl2\nl3\nl4\n")
	m = typeKeys(m, "x")   // line 0
	m = typeKeys(m, "jjx") // line 2
	m = typeKeys(m, "jjx") // line 4
	m = typeKeys(m, "gg")
	m = typeKeys(m, "2g;")
	if l, _ := m.CursorPos(); l != 2 {
		t.Fatalf("2g; on line %d, want 2", l)
	}
	m = typeKeys(m, "9g;") // clamps at the oldest
	if l, _ := m.CursorPos(); l != 0 {
		t.Fatalf("9g; on line %d, want 0", l)
	}
}

// TestChangeListGCommaWithoutWalk: g, before any g; has nothing newer.
func TestChangeListGCommaWithoutWalk(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\n")
	m = typeKeys(m, "x")
	m = typeKeys(m, "j")
	m = typeKeys(m, "g,")
	if l, _ := m.CursorPos(); l != 1 {
		t.Fatalf("g, without a walk moved to line %d", l)
	}
	if !strings.Contains(m.cmdMsg, "no later edit position") {
		t.Fatalf("cmdMsg = %q, want no later edit position", m.cmdMsg)
	}
}

// TestChangeListEmpty: g; on an unedited buffer reports and stays put.
func TestChangeListEmpty(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\n")
	m = typeKeys(m, "j")
	m = typeKeys(m, "g;")
	if l, _ := m.CursorPos(); l != 1 {
		t.Fatalf("g; on an empty list moved to line %d", l)
	}
	if !strings.Contains(m.cmdMsg, "no earlier edit position") {
		t.Fatalf("cmdMsg = %q, want no earlier edit position", m.cmdMsg)
	}
}

// TestChangeListDedupesAdjacentSameLine: a run of edits on one line collapses
// into a single entry that keeps the newest position.
func TestChangeListDedupesAdjacentSameLine(t *testing.T) {
	m, _ := loaded(t, "abcdef\nl1\n")
	m = typeKeys(m, "xxx") // three changes, all on line 0
	if n := len(m.changes.pos); n != 1 {
		t.Fatalf("ring has %d entries, want 1", n)
	}
	m = typeKeys(m, "j")
	m = typeKeys(m, "g;")
	if l, _ := m.CursorPos(); l != 0 {
		t.Fatalf("g; on line %d, want 0", l)
	}
	m = typeKeys(m, "g;")
	if !strings.Contains(m.cmdMsg, "no earlier edit position") {
		t.Fatalf("cmdMsg = %q — the collapsed run must be one stop", m.cmdMsg)
	}
}

// TestChangeListPointerResetsOnNewEdit: after walking back, a fresh edit
// resets the pointer so the next g; starts from the newest position again.
func TestChangeListPointerResetsOnNewEdit(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\nl2\nl3\n")
	m = typeKeys(m, "x")   // line 0
	m = typeKeys(m, "jjx") // line 2
	m = typeKeys(m, "g;")  // walk: no move (already on line 2), pointer at 1
	m = typeKeys(m, "g;")  // line 0, pointer at 2
	if l, _ := m.CursorPos(); l != 0 {
		t.Fatalf("setup: g; g; on line %d, want 0", l)
	}
	m = typeKeys(m, "jjjx") // new edit on line 3 resets the walk
	m = typeKeys(m, "gg")
	m = typeKeys(m, "g;")
	if l, _ := m.CursorPos(); l != 3 {
		t.Fatalf("g; after a new edit on line %d, want 3 (the newest)", l)
	}
	if !strings.Contains(m.cmdMsg, "change list: 1/3") {
		t.Fatalf("cmdMsg = %q, want change list: 1/3", m.cmdMsg)
	}
}

// TestChangeListNoUndoNoNav: g; moves the cursor only — the buffer text is
// untouched and no EventJump (the nav-history seam) is emitted.
func TestChangeListNoUndoNoNav(t *testing.T) {
	m, _ := loaded(t, "abc\nl1\nl2\n")
	m = typeKeys(m, "x") // "bc" on line 0
	m = typeKeys(m, "jj")

	var jumps int
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventJump {
			jumps++
		}
	}))
	m = typeKeys(m, "g;")
	if l, _ := m.CursorPos(); l != 0 {
		t.Fatalf("g; on line %d, want 0", l)
	}
	if line(m, 0) != "bc" {
		t.Fatalf("g; changed the buffer: line 0 = %q", line(m, 0))
	}
	if jumps != 0 {
		t.Fatal("g; must not emit EventJump (no nav-history entry)")
	}
}

// TestChangeListUndoAddsNoEntry: u reverts text but records no change-list
// entry — only committed changes do.
func TestChangeListUndoAddsNoEntry(t *testing.T) {
	m, _ := loaded(t, "abc\nl1\n")
	m = typeKeys(m, "x")
	m = typeKeys(m, "u")
	if n := len(m.changes.pos); n != 1 {
		t.Fatalf("ring has %d entries after undo, want 1", n)
	}
}

// TestChangeListShiftsWithLineDelta: entries above a line deletion pull up
// with the same delta scheme marks use, so g; still lands on the edited text.
func TestChangeListShiftsWithLineDelta(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\nl2\nl3\nl4\n")
	m = typeKeys(m, "jjjjx") // edit on line 4 → entry at 4
	m = typeKeys(m, "gg")
	m = typeKeys(m, "dd") // delete line 0 → entry shifts to 3, dd records at 0
	m = typeKeys(m, "g;") // newest: the dd position (line 0)
	if l, _ := m.CursorPos(); l != 0 {
		t.Fatalf("1st g; on line %d, want 0", l)
	}
	m = typeKeys(m, "g;") // the shifted x edit
	if l, _ := m.CursorPos(); l != 3 {
		t.Fatalf("2nd g; on line %d, want 3 (shifted from 4)", l)
	}
	if line(m, 3) != "4" { // "l4" with the l deleted
		t.Fatalf("g; landed on %q, want the edited line", line(m, 3))
	}
}

// TestChangeListClampsIntoBuffer: an entry past the end of a shrunken buffer
// clamps to a valid position instead of landing outside the text.
func TestChangeListClampsIntoBuffer(t *testing.T) {
	m, _ := loaded(t, "l0\nl1\nl2\nl3\n")
	m = typeKeys(m, "jjjx") // entry at line 3
	m = typeKeys(m, "gg")
	m = typeKeys(m, "jVGd") // delete lines 1..3 (one change at line 1)
	m = typeKeys(m, "g;")   // newest: the delete
	m = typeKeys(m, "g;")   // the old entry, clamped into the buffer
	if l, _ := m.CursorPos(); l < 0 || l >= m.buf.LineCount() {
		t.Fatalf("g; landed outside the buffer: line %d of %d", l, m.buf.LineCount())
	}
}

// TestChangeListResetsOnLoad: loading a file drops the previous ring with the
// undo history.
func TestChangeListResetsOnLoad(t *testing.T) {
	m, path := loaded(t, "l0\nl1\n")
	m = typeKeys(m, "x")
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if n := len(m.changes.pos); n != 0 {
		t.Fatalf("ring has %d entries after Load, want 0", n)
	}
}

// TestChangeListCap: the ring stays bounded at changeListMax.
func TestChangeListCap(t *testing.T) {
	var c changeList
	for i := 0; i < changeListMax+20; i++ {
		c.record(buffer.Position{Line: i})
	}
	if len(c.pos) != changeListMax {
		t.Fatalf("ring len = %d, want %d", len(c.pos), changeListMax)
	}
	if c.pos[0].Line != 20 {
		t.Fatalf("oldest entry line = %d, want 20 (oldest dropped first)", c.pos[0].Line)
	}
}

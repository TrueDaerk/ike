package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// --- undo tree (#59): g-/g+, branch keeping, overlay wiring -----------------

func TestChronoUndoRedoKeys(t *testing.T) {
	m, _ := loaded(t, "a\n")
	m = typeKeys(m, "Ab")
	m = send(m, special(tea.KeyEscape)) // seq 1: "ab"
	m = typeKeys(m, "u")                // back to "a"
	m = typeKeys(m, "Ac")
	m = send(m, special(tea.KeyEscape)) // seq 2: "ac", sibling branch

	// g- steps chronologically: from seq 2 to seq 1, across the branch.
	m = typeKeys(m, "g-")
	if line(m, 0) != "ab" {
		t.Fatalf("g- landed on %q, want ab (the other branch)", line(m, 0))
	}
	m = typeKeys(m, "g-")
	if line(m, 0) != "a" {
		t.Fatalf("second g- landed on %q, want a", line(m, 0))
	}
	// g+ walks forward again in seq order.
	m = typeKeys(m, "g+")
	if line(m, 0) != "ab" {
		t.Fatalf("g+ landed on %q, want ab", line(m, 0))
	}
	m = typeKeys(m, "g+")
	if line(m, 0) != "ac" {
		t.Fatalf("second g+ landed on %q, want ac", line(m, 0))
	}
}

func TestDivergentEditKeepsBranch(t *testing.T) {
	m, _ := loaded(t, "a\n")
	m = typeKeys(m, "Ab")
	m = send(m, special(tea.KeyEscape)) // seq 1: "ab"
	m = typeKeys(m, "u")
	m = typeKeys(m, "Ac")
	m = send(m, special(tea.KeyEscape)) // seq 2: "ac" — before #59 this dropped seq 1

	tree := m.HistoryTree()
	if len(tree) != 3 {
		t.Fatalf("tree has %d nodes, want 3 (root + both branches)", len(tree))
	}

	// The overlay's jump restores the abandoned branch.
	m, _ = m.Update(HistoryJumpMsg{Seq: 1})
	if line(m, 0) != "ab" {
		t.Fatalf("jump to seq 1 landed on %q, want ab", line(m, 0))
	}
	if m.HistoryTree()[1].Current != true {
		t.Fatal("seq 1 should be marked current after the jump")
	}
}

func TestUndoTreeActionOpensOverlay(t *testing.T) {
	m, _ := loaded(t, "a\n")
	m, cmd := m.Update(ActionMsg{Action: "undo_tree"})
	if cmd == nil {
		t.Fatal("undo_tree action should emit a command")
	}
	if _, ok := cmd().(OpenUndoTreeMsg); !ok {
		t.Fatalf("undo_tree action emitted %T, want OpenUndoTreeMsg", cmd())
	}
	_ = m
}

func TestJumpTracksSavedState(t *testing.T) {
	m, path := loaded(t, "a\n")
	m = typeKeys(m, "Ab")
	m = send(m, special(tea.KeyEscape)) // seq 1: "ab"
	if err := m.saveAs(path); err != nil {
		t.Fatal(err)
	}
	m = typeKeys(m, "u") // back to "a", dirty vs. disk
	if !m.Dirty() {
		t.Fatal("undoing below the save point must be dirty")
	}
	m, _ = m.Update(HistoryJumpMsg{Seq: 1})
	if m.Dirty() {
		t.Fatal("jumping back to the saved state must clear dirty")
	}
}

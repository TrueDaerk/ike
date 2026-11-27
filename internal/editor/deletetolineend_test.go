package editor

import (
	"testing"

	"ike/internal/editor/buffer"
)

func TestDeleteToLineEndMidLine(t *testing.T) {
	m, _ := loaded(t, "alpha beta\ngamma\n")
	m.cursor = buffer.Position{Line: 0, Col: 5}

	m, _ = m.runAction("delete_to_line_end")

	if got := m.Text(); got != "alpha\ngamma" {
		t.Fatalf("text = %q, want the tail gone and the line break kept", got)
	}
	if m.cursor.Line != 0 {
		t.Fatalf("cursor = %v, want line 0", m.cursor)
	}
}

func TestDeleteToLineEndAtLineEndJoins(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\n")
	m.cursor = buffer.Position{Line: 0, Col: 4}
	m, _ = m.Update(modKey('a', 0)) // append: insert at the line end

	m, _ = m.runAction("delete_to_line_end")

	if got := m.Text(); got != "alphabeta" {
		t.Fatalf("text = %q, want the next line joined", got)
	}
	if m.ModeName() != Insert {
		t.Fatalf("mode = %v, want Insert", m.ModeName())
	}
	if m.cursor != (buffer.Position{Line: 0, Col: 5}) {
		t.Fatalf("cursor = %v, want line 0 col 5", m.cursor)
	}
}

func TestDeleteToLineEndLastLineEndIsNoOp(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta")
	m.cursor = buffer.Position{Line: 1, Col: 3}
	m, _ = m.Update(modKey('a', 0))

	m, _ = m.runAction("delete_to_line_end")

	if got := m.Text(); got != "alpha\nbeta" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
}

func TestDeleteToLineEndEmptyLineJoins(t *testing.T) {
	m, _ := loaded(t, "alpha\n\nbeta\n")
	m.cursor = buffer.Position{Line: 1}

	m, _ = m.runAction("delete_to_line_end")

	if got := m.Text(); got != "alpha\nbeta" {
		t.Fatalf("text = %q, want the empty line joined with the next", got)
	}
}

func TestDeleteToLineEndMultiCaretOneUndo(t *testing.T) {
	m, _ := loaded(t, "one two\nthree four\nfive\n")
	m.cursor = buffer.Position{Line: 0, Col: 3}
	m.addCaret(buffer.Position{Line: 1, Col: 5})

	m, _ = m.runAction("delete_to_line_end")

	if got := m.Text(); got != "one\nthree\nfive" {
		t.Fatalf("text = %q, want every caret's tail gone", got)
	}

	m, _ = m.runAction("undo")
	if got := m.Text(); got != "one two\nthree four\nfive" {
		t.Fatalf("after one undo = %q, want the original text", got)
	}
}

func TestDeleteToLineEndInsertUndoRestores(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m.cursor = buffer.Position{Line: 0, Col: 5}
	m, _ = m.Update(key('i'))

	m, _ = m.runAction("delete_to_line_end")
	if got := m.Text(); got != "alpha" {
		t.Fatalf("text = %q", got)
	}
	m, _ = m.runAction("undo")
	if got := m.Text(); got != "alpha beta" {
		t.Fatalf("after undo = %q, want the original text", got)
	}
}

func TestDeleteToLineEndReadOnlyRefuses(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m.cursor = buffer.Position{Line: 0, Col: 2}
	m.SetReadOnly(true)

	m, _ = m.runAction("delete_to_line_end")

	if got := m.Text(); got != "alpha beta" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
	if m.cmdMsg != roMessage {
		t.Fatalf("cmdMsg = %q, want the read-only notice", m.cmdMsg)
	}
}

package history

import (
	"testing"

	"ike/internal/editor/buffer"
)

// amendBuf is a one-line buffer the amend tests edit.
func amendBuf(t *testing.T, line string) *buffer.Buffer {
	t.Helper()
	return buffer.New([]string{line})
}

// change records applying e against b as an undoable Change.
func change(b *buffer.Buffer, e buffer.Edit) Change {
	inv, _ := b.Apply(e)
	return Change{Forwards: []buffer.Edit{e}, Inverses: []buffer.Edit{inv}}
}

// insertAt builds an insertion edit of text at line 0, column col.
func insertAt(col int, text string) buffer.Edit {
	pos := buffer.Position{Line: 0, Col: col}
	return buffer.Edit{Range: buffer.Range{Start: pos, End: pos}, Text: text}
}

// TestAmendMergesIntoOneUndoUnit is the #2253 contract: the save chain's
// organize-imports and format rewrites merge into a single change, so one undo
// reverts both — and does so in the right order (format first).
func TestAmendMergesIntoOneUndoUnit(t *testing.T) {
	b := amendBuf(t, "x")
	h := New()

	h.Push(change(b, insertAt(0, "a")))
	if !h.Amend(change(b, insertAt(0, "b"))) {
		t.Fatal("Amend after a pushed change must merge")
	}
	if got := b.Lines()[0]; got != "bax" {
		t.Fatalf("buffer after both edits = %q, want %q", got, "bax")
	}
	if _, ok := h.Undo(b); !ok {
		t.Fatal("merged unit must be undoable")
	}
	if got := b.Lines()[0]; got != "x" {
		t.Fatalf("one undo must revert both edits, got %q", got)
	}
	if h.CanUndo() {
		t.Fatal("both edits were one unit: nothing left to undo")
	}
	if _, ok := h.Redo(b); !ok || b.Lines()[0] != "bax" {
		t.Fatalf("redo must replay the merged unit, got %q", b.Lines()[0])
	}
}

// TestAmendAtRootPushes: with no change to merge into, the caller must fall
// back to Push — Amend reports that by returning false and leaving the store
// untouched.
func TestAmendAtRootPushes(t *testing.T) {
	b := amendBuf(t, "x")
	h := New()
	if h.Amend(change(b, insertAt(0, "a"))) {
		t.Fatal("Amend at the root state must report false")
	}
	if h.CanUndo() {
		t.Fatal("a refused Amend must not create a state")
	}
}

// TestAmendRefusedWithChildren protects the undo tree: a state other branches
// hang off must never be rewritten under them.
func TestAmendRefusedWithChildren(t *testing.T) {
	b := amendBuf(t, "x")
	h := New()
	h.Push(change(b, insertAt(0, "a")))
	h.Push(change(b, insertAt(0, "b")))
	if _, ok := h.Undo(b); !ok {
		t.Fatal("undo must step back onto the state with a child")
	}
	if h.Amend(change(b, insertAt(0, "c"))) {
		t.Fatal("a state with children must refuse the merge")
	}
}

// TestAmendClearsSavedMarker: rewriting the state the file was written from
// means no state matches disk anymore — undoing back to it must not report
// clean.
func TestAmendClearsSavedMarker(t *testing.T) {
	b := amendBuf(t, "x")
	h := New()
	h.Push(change(b, insertAt(0, "a")))
	h.MarkSaved()
	if !h.AtSaved() {
		t.Fatal("the pushed state was just marked saved")
	}
	if !h.Amend(change(b, insertAt(0, "b"))) {
		t.Fatal("Amend must merge")
	}
	if h.AtSaved() {
		t.Fatal("an amended state no longer matches the written file")
	}
}

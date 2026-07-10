package history

import (
	"testing"

	"ike/internal/editor/buffer"
)

func TestUndoRedoRoundTrip(t *testing.T) {
	b := buffer.FromString("abc")
	h := New()

	rec := NewRecorder(b, buffer.Position{Line: 0, Col: 0})
	end := rec.Apply(buffer.Insert(buffer.Position{Line: 0, Col: 3}, "def"))
	h.Push(rec.Commit(end))
	if b.Line(0) != "abcdef" {
		t.Fatalf("after edit=%q", b.Line(0))
	}

	cur, ok := h.Undo(b)
	if !ok || b.Line(0) != "abc" {
		t.Fatalf("undo: ok=%v line=%q", ok, b.Line(0))
	}
	if cur != (buffer.Position{Line: 0, Col: 0}) {
		t.Fatalf("undo cursor=%v want {0 0}", cur)
	}

	cur, ok = h.Redo(b)
	if !ok || b.Line(0) != "abcdef" {
		t.Fatalf("redo: ok=%v line=%q", ok, b.Line(0))
	}
	if cur != end {
		t.Fatalf("redo cursor=%v want %v", cur, end)
	}
}

func TestUndoMultiEditChange(t *testing.T) {
	b := buffer.FromString("hello world")
	h := New()
	rec := NewRecorder(b, buffer.Position{Line: 0, Col: 0})
	rec.Apply(buffer.Delete(buffer.NewRange(buffer.Position{Line: 0, Col: 0}, buffer.Position{Line: 0, Col: 6})))
	rec.Apply(buffer.Insert(buffer.Position{Line: 0, Col: 0}, "HELLO "))
	h.Push(rec.Commit(buffer.Position{Line: 0, Col: 6}))
	if b.Line(0) != "HELLO world" {
		t.Fatalf("after=%q", b.Line(0))
	}
	h.Undo(b)
	if b.Line(0) != "hello world" {
		t.Fatalf("undo multi=%q", b.Line(0))
	}
}

func TestPushClearsRedo(t *testing.T) {
	b := buffer.FromString("x")
	h := New()
	r1 := NewRecorder(b, buffer.Position{Line: 0, Col: 0})
	r1.Apply(buffer.Insert(buffer.Position{Line: 0, Col: 1}, "y"))
	h.Push(r1.Commit(buffer.Position{Line: 0, Col: 1}))
	h.Undo(b)
	if !h.CanRedo() {
		t.Fatal("should be able to redo")
	}
	r2 := NewRecorder(b, buffer.Position{Line: 0, Col: 0})
	r2.Apply(buffer.Insert(buffer.Position{Line: 0, Col: 1}, "z"))
	h.Push(r2.Commit(buffer.Position{Line: 0, Col: 1}))
	if h.CanRedo() {
		t.Fatal("new edit should clear redo stack")
	}
}

func TestUndoEmptyIsSafe(t *testing.T) {
	b := buffer.FromString("a")
	h := New()
	if _, ok := h.Undo(b); ok {
		t.Fatal("undo on empty history should report ok=false")
	}
}

func TestSavedCheckpoint(t *testing.T) {
	b := buffer.FromString("abc")
	h := New()
	if !h.AtSaved() {
		t.Fatal("a fresh history should start at the saved state")
	}

	rec := NewRecorder(b, buffer.Position{})
	end := rec.Apply(buffer.Insert(buffer.Position{Line: 0, Col: 3}, "d"))
	h.Push(rec.Commit(end))
	if h.AtSaved() {
		t.Fatal("a pushed change should leave the saved state")
	}

	h.MarkSaved()
	if !h.AtSaved() {
		t.Fatal("MarkSaved should pin the current state")
	}
	if _, ok := h.Undo(b); !ok || h.AtSaved() {
		t.Fatal("undoing away from the save point should not be AtSaved")
	}
	if _, ok := h.Redo(b); !ok || !h.AtSaved() {
		t.Fatal("redoing back to the save point should be AtSaved")
	}
}

func TestSavedCheckpointDiscardedByBranch(t *testing.T) {
	b := buffer.FromString("abc")
	h := New()
	rec := NewRecorder(b, buffer.Position{})
	end := rec.Apply(buffer.Insert(buffer.Position{Line: 0, Col: 3}, "d"))
	h.Push(rec.Commit(end))
	h.MarkSaved()
	if _, ok := h.Undo(b); !ok {
		t.Fatal("undo failed")
	}
	rec = NewRecorder(b, buffer.Position{})
	end = rec.Apply(buffer.Insert(buffer.Position{Line: 0, Col: 3}, "e"))
	h.Push(rec.Commit(end)) // discards the future holding the saved state
	if _, ok := h.Undo(b); !ok {
		t.Fatal("undo failed")
	}
	if h.AtSaved() {
		t.Fatal("the saved state was discarded; AtSaved must stay false")
	}
}

func TestMarkNeverSaved(t *testing.T) {
	h := New()
	h.MarkNeverSaved()
	if h.AtSaved() {
		t.Fatal("MarkNeverSaved: even the empty state must not read as saved")
	}
	h.Reset()
	if !h.AtSaved() {
		t.Fatal("Reset re-baselines the empty state as saved")
	}
}

package history

import (
	"encoding/json"
	"testing"

	"ike/internal/editor/buffer"
)

// pushEdit applies an insert to b and records it as one change on h.
func pushEdit(t *testing.T, h *History, b *buffer.Buffer, pos buffer.Position, text string) {
	t.Helper()
	e := buffer.Insert(pos, text)
	inv, end := b.Apply(e)
	h.Push(Change{
		Forwards:     []buffer.Edit{e},
		Inverses:     []buffer.Edit{inv},
		CursorBefore: pos,
		CursorAfter:  end,
	})
}

func TestSnapshotRoundTrip(t *testing.T) {
	b := buffer.FromString("hello")
	h := New()
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 5}, " world")
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 11}, "!")
	if _, ok := h.Undo(b); !ok { // one change on future, one on past
		t.Fatal("undo failed")
	}

	data, err := json.Marshal(h.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}

	// A fresh history over a buffer holding the same content adopts the stacks.
	b2 := buffer.FromString(b.String())
	h2 := New()
	h2.RestoreSnapshot(snap)
	if !h2.AtSaved() {
		t.Error("restored state should count as saved")
	}
	if !h2.CanUndo() || !h2.CanRedo() {
		t.Fatalf("restored stacks incomplete: undo=%v redo=%v", h2.CanUndo(), h2.CanRedo())
	}
	if _, ok := h2.Redo(b2); !ok {
		t.Fatal("redo failed")
	}
	if got := b2.String(); got != "hello world!" {
		t.Errorf("after redo: %q", got)
	}
	if _, ok := h2.Undo(b2); !ok {
		t.Fatal("undo failed")
	}
	if _, ok := h2.Undo(b2); !ok {
		t.Fatal("undo failed")
	}
	if got := b2.String(); got != "hello" {
		t.Errorf("after full undo: %q", got)
	}
}

func TestRestoreSnapshotResumesSeq(t *testing.T) {
	b := buffer.FromString("x")
	h := New()
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 1}, "y")
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 2}, "z")

	h2 := New()
	h2.RestoreSnapshot(h.Snapshot())
	pushEdit(t, h2, b, buffer.Position{Line: 0, Col: 3}, "!")

	s := h2.Snapshot()
	last := s.Nodes[len(s.Nodes)-1]
	if last.Seq != 3 {
		t.Errorf("seq after restore = %d, want 3", last.Seq)
	}
	if last.Parent != 2 {
		t.Errorf("parent after restore = %d, want 2", last.Parent)
	}
}

func TestRestoreEmptySnapshot(t *testing.T) {
	h := New()
	h.RestoreSnapshot(Snapshot{})
	if h.CanUndo() || h.CanRedo() {
		t.Error("empty snapshot must restore an empty history")
	}
	if !h.AtSaved() {
		t.Error("empty restored history counts as saved")
	}
}

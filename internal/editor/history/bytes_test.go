package history

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// bigChange fabricates a whole-buffer-sized change: n bytes of forward text
// plus n bytes of inverse text, the shape a reformat-on-save or :%s produces.
func bigChange(n int) Change {
	text := strings.Repeat("x", n)
	return Change{
		Forwards: []buffer.Edit{{Text: text}},
		Inverses: []buffer.Edit{{Text: text}},
	}
}

func TestByteBudgetBoundsRetainedText(t *testing.T) {
	h := New()
	per := 4 << 20 // 8 MiB retained per change (forwards + inverses)
	for i := 0; i < 40; i++ {
		h.Push(bigChange(per))
	}
	if h.bytes > maxBytes {
		t.Fatalf("retained %d bytes, budget %d", h.bytes, maxBytes)
	}
	if len(h.nodes) >= 40 {
		t.Fatalf("expected pruning, still %d nodes", len(h.nodes))
	}
	if !h.CanUndo() {
		t.Fatal("newest change must survive the budget")
	}
}

func TestOversizedSingleChangeSurvives(t *testing.T) {
	h := New()
	h.Push(bigChange(maxBytes)) // 2x maxBytes retained
	if len(h.nodes) != 1 || !h.CanUndo() {
		t.Fatalf("oversized change must stay: nodes=%d canUndo=%v", len(h.nodes), h.CanUndo())
	}
	// The next push prunes the oversized level; the budget recovers.
	h.Push(bigChange(1024))
	if h.bytes > maxBytes {
		t.Fatalf("retained %d bytes after follow-up push, budget %d", h.bytes, maxBytes)
	}
	if !h.CanUndo() {
		t.Fatal("latest change must remain undoable")
	}
}

func TestByteAccountingBalancesOnPrune(t *testing.T) {
	h := New()
	for i := 0; i < 50; i++ {
		h.Push(bigChange(2 << 20))
	}
	want := 0
	for _, c := range h.nodes {
		want += cost(c)
	}
	if h.bytes != want {
		t.Fatalf("bytes=%d, sum over nodes=%d", h.bytes, want)
	}
}

func TestRestoreSnapshotAppliesBudget(t *testing.T) {
	h := New()
	var records []ChangeRecord
	for i := 1; i <= 20; i++ {
		c := bigChange(4 << 20)
		c.seq, c.parent = i, i-1
		records = append(records, toRecord(c))
	}
	h.RestoreSnapshot(Snapshot{Nodes: records, Current: 20})
	if h.bytes > maxBytes {
		t.Fatalf("restored history retains %d bytes, budget %d", h.bytes, maxBytes)
	}
	if h.current != 20 || !h.CanUndo() {
		t.Fatalf("current state lost: current=%d canUndo=%v", h.current, h.CanUndo())
	}
}

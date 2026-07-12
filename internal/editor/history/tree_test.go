package history

import (
	"testing"

	"ike/internal/editor/buffer"
)

// TestBranchOnDivergentEdit: a push after an undo keeps the old future as a
// sibling branch instead of discarding it, and redo follows the new branch.
func TestBranchOnDivergentEdit(t *testing.T) {
	b := buffer.FromString("a")
	h := New()
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 1}, "b") // seq 1: "ab"
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 2}, "c") // seq 2: "abc"
	if _, ok := h.Undo(b); !ok {
		t.Fatal("undo failed")
	}
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 2}, "X") // seq 3: "abX", sibling of 2

	tree := h.Tree()
	if len(tree) != 4 { // root + 3 changes
		t.Fatalf("tree has %d nodes, want 4", len(tree))
	}
	if tree[2].Parent != 1 || tree[3].Parent != 1 {
		t.Errorf("seq 2 and 3 should both branch from 1, got parents %d and %d",
			tree[2].Parent, tree[3].Parent)
	}
	if !tree[3].Current {
		t.Error("current should be seq 3")
	}

	// Undo back below the branch point, then redo: the new branch is active.
	if _, ok := h.Undo(b); !ok {
		t.Fatal("undo failed")
	}
	if got := b.String(); got != "ab" {
		t.Fatalf("after undo: %q", got)
	}
	if _, ok := h.Redo(b); !ok {
		t.Fatal("redo failed")
	}
	if got := b.String(); got != "abX" {
		t.Errorf("redo should follow the active branch: %q", got)
	}
}

// TestJumpToNode: jumping across branches applies inverses up to the common
// ancestor and forwards down to the target.
func TestJumpToNode(t *testing.T) {
	b := buffer.FromString("a")
	h := New()
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 1}, "b") // seq 1: "ab"
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 2}, "c") // seq 2: "abc"
	h.Undo(b)
	h.Undo(b)                                                // back to "a"
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 1}, "Z") // seq 3: "aZ", branch at root

	// Jump from seq 3 to seq 2 (other branch).
	cur, ok := h.JumpTo(b, 2)
	if !ok {
		t.Fatal("jump failed")
	}
	if got := b.String(); got != "abc" {
		t.Errorf("after jump to 2: %q", got)
	}
	if cur != (buffer.Position{Line: 0, Col: 3}) {
		t.Errorf("cursor after jump = %+v", cur)
	}
	if h.CurrentSeq() != 2 {
		t.Errorf("current = %d, want 2", h.CurrentSeq())
	}

	// Jump to an ancestor (pure undo walk).
	if _, ok := h.JumpTo(b, 1); !ok {
		t.Fatal("jump to ancestor failed")
	}
	if got := b.String(); got != "ab" {
		t.Errorf("after jump to 1: %q", got)
	}

	// Jump to the root.
	if _, ok := h.JumpTo(b, 0); !ok {
		t.Fatal("jump to root failed")
	}
	if got := b.String(); got != "a" {
		t.Errorf("after jump to root: %q", got)
	}

	// Jumping to the current or an unknown state is a no-op.
	if _, ok := h.JumpTo(b, 0); ok {
		t.Error("jump to current must report false")
	}
	if _, ok := h.JumpTo(b, 99); ok {
		t.Error("jump to unknown seq must report false")
	}
}

// TestChronoOrder: g-/g+ walk states in global seq order across branches.
func TestChronoOrder(t *testing.T) {
	b := buffer.FromString("a")
	h := New()
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 1}, "b") // seq 1: "ab"
	h.Undo(b)
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 1}, "c") // seq 2: "ac", sibling of 1

	// g-: from seq 2 the chronologically previous state is seq 1 ("ab"),
	// which lives on the other branch.
	if _, ok := h.UndoChrono(b); !ok {
		t.Fatal("g- failed")
	}
	if got := b.String(); got != "ab" {
		t.Errorf("after g-: %q (want the other branch's state)", got)
	}
	// g- again: root.
	if _, ok := h.UndoChrono(b); !ok {
		t.Fatal("g- to root failed")
	}
	if got := b.String(); got != "a" {
		t.Errorf("after second g-: %q", got)
	}
	// g- at the oldest state: nothing.
	if _, ok := h.UndoChrono(b); ok {
		t.Error("g- at root must report false")
	}
	// g+ twice walks forward in seq order: 1 then 2.
	if _, ok := h.RedoChrono(b); !ok {
		t.Fatal("g+ failed")
	}
	if got := b.String(); got != "ab" {
		t.Errorf("after g+: %q", got)
	}
	if _, ok := h.RedoChrono(b); !ok {
		t.Fatal("second g+ failed")
	}
	if got := b.String(); got != "ac" {
		t.Errorf("after second g+: %q", got)
	}
	if _, ok := h.RedoChrono(b); ok {
		t.Error("g+ at newest state must report false")
	}
}

// TestSavedTracking: AtSaved follows the current node across tree walks.
func TestSavedTracking(t *testing.T) {
	b := buffer.FromString("a")
	h := New()
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 1}, "b")
	h.MarkSaved()
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 2}, "c")
	if h.AtSaved() {
		t.Error("new change must leave the saved state")
	}
	h.Undo(b)
	if !h.AtSaved() {
		t.Error("undo back to the saved state must report AtSaved")
	}
	h.Undo(b)
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 1}, "X") // branch
	if h.AtSaved() {
		t.Error("divergent branch is not the saved state")
	}
	if _, ok := h.JumpTo(b, 1); !ok {
		t.Fatal("jump failed")
	}
	if !h.AtSaved() {
		t.Error("jumping back to the saved node must report AtSaved")
	}
}

// TestPruneOldestBranch: past the node cap, the oldest leaf branch goes first.
func TestPruneOldestBranch(t *testing.T) {
	b := buffer.FromString("")
	h := New()
	// One abandoned branch (seq 1), then a long linear run.
	pushEdit(t, h, b, buffer.Position{}, "x") // seq 1
	h.Undo(b)
	for i := 0; i < maxNodes; i++ { // seq 2..maxNodes+1
		pushEdit(t, h, b, buffer.Position{}, "y")
	}
	tree := h.Tree()
	if len(tree) != maxNodes+1 { // root + maxNodes changes
		t.Fatalf("tree has %d nodes, want %d", len(tree), maxNodes+1)
	}
	for _, n := range tree {
		if n.Seq == 1 {
			t.Fatal("the abandoned oldest branch should have been pruned")
		}
	}
}

// TestPruneLinearDropsOldestLevel: a purely linear history over the cap drops
// its oldest level; the remaining chain still undoes cleanly to the new root.
func TestPruneLinearDropsOldestLevel(t *testing.T) {
	b := buffer.FromString("")
	h := New()
	for i := 0; i < maxNodes+5; i++ {
		pushEdit(t, h, b, buffer.Position{}, "y")
	}
	if got := len(h.Tree()); got != maxNodes+1 {
		t.Fatalf("tree has %d nodes, want %d", got, maxNodes+1)
	}
	undos := 0
	for {
		if _, ok := h.Undo(b); !ok {
			break
		}
		undos++
	}
	if undos != maxNodes {
		t.Errorf("chain undoes %d levels, want %d", undos, maxNodes)
	}
	if got := len(b.String()); got != 5 {
		t.Errorf("new root state holds %d chars, want 5 (the dropped levels)", got)
	}
}

// TestLegacySnapshotRestore: a pre-tree snapshot (past/future stacks) restores
// into an equivalent chain.
func TestLegacySnapshotRestore(t *testing.T) {
	b := buffer.FromString("hello")
	h := New()
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 5}, " world") // seq 1
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 11}, "!")     // seq 2
	h.Undo(b)                                                     // "hello world", seq 2 on the redo side
	tree := h.Snapshot()

	legacy := Snapshot{Past: tree.Nodes[:1], Future: tree.Nodes[1:]}
	b2 := buffer.FromString(b.String())
	h2 := New()
	h2.RestoreSnapshot(legacy)
	if h2.CurrentSeq() != 1 {
		t.Fatalf("restored current = %d, want 1", h2.CurrentSeq())
	}
	if _, ok := h2.Redo(b2); !ok {
		t.Fatal("redo after legacy restore failed")
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

// TestTreeSnapshotRoundTripWithBranches: the tree wire form preserves branch
// structure and the current node.
func TestTreeSnapshotRoundTripWithBranches(t *testing.T) {
	b := buffer.FromString("a")
	h := New()
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 1}, "b") // seq 1
	h.Undo(b)
	pushEdit(t, h, b, buffer.Position{Line: 0, Col: 1}, "c") // seq 2, sibling

	b2 := buffer.FromString(b.String())
	h2 := New()
	h2.RestoreSnapshot(h.Snapshot())
	if h2.CurrentSeq() != 2 {
		t.Fatalf("restored current = %d, want 2", h2.CurrentSeq())
	}
	if !h2.AtSaved() {
		t.Error("restored state counts as saved")
	}
	// The other branch is still reachable.
	if _, ok := h2.JumpTo(b2, 1); !ok {
		t.Fatal("jump to the preserved branch failed")
	}
	if got := b2.String(); got != "ab" {
		t.Errorf("after jump: %q", got)
	}
}

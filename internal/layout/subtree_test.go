package layout

import "testing"

// subtree_test.go covers the subtree helpers behind the #3109 editor anchor.

func threeRow() (*Split, *Split, *Split) {
	bottom := &Split{Orient: Horizontal, Ratio: 0.5, A: &Leaf{Pane: "c"}, B: &Leaf{Pane: "h"}}
	root := &Split{Orient: Vertical, Ratio: 2.0 / 3,
		A: &Split{Orient: Horizontal, Ratio: 0.2, A: &Leaf{Pane: "e"}, B: &Leaf{Pane: "i"}},
		B: bottom}
	return root, root.A.(*Split), bottom
}

func TestCommonAncestor(t *testing.T) {
	root, top, bottom := threeRow()
	if got := CommonAncestor(root, map[string]bool{"c": true, "h": true}); got != bottom {
		t.Fatalf("c+h must resolve to the bottom split, got %v", got)
	}
	if got := CommonAncestor(root, map[string]bool{"e": true}); got.(*Leaf).Pane != "e" {
		t.Fatalf("a single leaf resolves to itself, got %v", got)
	}
	if got := CommonAncestor(root, map[string]bool{"e": true, "h": true}); got != root {
		t.Fatalf("e+h span the root, got %v", got)
	}
	if got := CommonAncestor(root, map[string]bool{"zz": true}); got != nil {
		t.Fatalf("unknown leaves yield nil, got %v", got)
	}
	if got := CommonAncestor(root, nil); got != nil {
		t.Fatalf("no leaves yield nil, got %v", got)
	}
	_ = top
}

func TestSplitNodeWrapsSubtree(t *testing.T) {
	root, _, bottom := threeRow()
	out, ok := SplitNode(root, bottom, "new", ZoneTop)
	if !ok || out != root {
		t.Fatalf("splitting an inner subtree keeps the root, ok=%v", ok)
	}
	wrapped, isSplit := root.B.(*Split)
	if !isSplit || wrapped.Orient != Vertical {
		t.Fatalf("the bottom strip must be wrapped in a vertical split, got %v", root.B)
	}
	if l, ok := wrapped.A.(*Leaf); !ok || l.Pane != "new" {
		t.Fatalf("ZoneTop puts the new leaf first, got %v", wrapped.A)
	}
	if wrapped.B != bottom {
		t.Fatal("the wrapped subtree must stay intact")
	}
	leaves := Leaves(root)
	if len(leaves) != 5 {
		t.Fatalf("leaves = %v", leaves)
	}
}

func TestSplitNodeAtRootAndMisses(t *testing.T) {
	root, _, _ := threeRow()
	out, ok := SplitNode(root, root, "new", ZoneRight)
	if !ok {
		t.Fatal("the root can be wrapped")
	}
	s := out.(*Split)
	if s.Orient != Horizontal || s.A != root || s.B.(*Leaf).Pane != "new" {
		t.Fatalf("ZoneRight wraps root as A with the new leaf as B, got %+v", s)
	}
	if _, ok := SplitNode(root, &Leaf{Pane: "e"}, "new", ZoneRight); ok {
		t.Fatal("a foreign node (same pane id, different identity) must not match")
	}
	if _, ok := SplitNode(root, root, "", ZoneRight); ok {
		t.Fatal("an empty pane id must fail")
	}
}

func TestBoundsAndOverlap(t *testing.T) {
	root, _, bottom := threeRow()
	rects := Rects(root, Rect{X: 0, Y: 0, W: 100, H: 30})
	b, ok := Bounds(bottom, rects)
	if !ok || b.X != 0 || b.W != 100 || b.Y != 20 || b.H != 10 {
		t.Fatalf("bottom bounds = %+v ok=%v", b, ok)
	}
	if _, ok := Bounds(&Leaf{Pane: "zz"}, rects); ok {
		t.Fatal("a leaf without a rect has no bounds")
	}
	if got := Overlap(Rect{0, 0, 10, 10}, Rect{5, 5, 10, 10}); got != 25 {
		t.Fatalf("overlap = %d, want 25", got)
	}
	if got := Overlap(Rect{0, 0, 10, 10}, Rect{10, 0, 10, 10}); got != 0 {
		t.Fatalf("touching rects overlap = %d, want 0", got)
	}
}

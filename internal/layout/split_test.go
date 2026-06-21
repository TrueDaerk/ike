package layout

import "testing"

// TestSplitLeafGrowsLeaf verifies SplitLeaf turns the target leaf into a split
// pairing it with the fresh leaf, ordered by zone.
func TestSplitLeafGrowsLeaf(t *testing.T) {
	root := Node(&Leaf{Pane: "editor"})
	out, ok := SplitLeaf(root, "editor", "editor:2", ZoneRight)
	if !ok {
		t.Fatal("SplitLeaf should succeed on an existing target")
	}
	s, isSplit := out.(*Split)
	if !isSplit || s.Orient != Horizontal {
		t.Fatalf("want horizontal split, got %#v", out)
	}
	if s.A.(*Leaf).Pane != "editor" || s.B.(*Leaf).Pane != "editor:2" {
		t.Fatalf("zone-right order wrong: A=%+v B=%+v", s.A, s.B)
	}
}

// TestSplitLeafZones checks each zone's orientation and child order.
func TestSplitLeafZones(t *testing.T) {
	cases := []struct {
		zone   Zone
		orient Orient
		aFirst bool // new leaf is A
	}{
		{ZoneLeft, Horizontal, true},
		{ZoneRight, Horizontal, false},
		{ZoneTop, Vertical, true},
		{ZoneBottom, Vertical, false},
	}
	for _, c := range cases {
		out, ok := SplitLeaf(&Leaf{Pane: "editor"}, "editor", "new", c.zone)
		if !ok {
			t.Fatalf("zone %d: split failed", c.zone)
		}
		s := out.(*Split)
		if s.Orient != c.orient {
			t.Fatalf("zone %d orient = %v", c.zone, s.Orient)
		}
		newIsA := s.A.(*Leaf).Pane == "new"
		if newIsA != c.aFirst {
			t.Fatalf("zone %d child order wrong: A=%+v", c.zone, s.A)
		}
	}
}

// TestSplitLeafMissingTarget leaves the tree unchanged when the target is absent.
func TestSplitLeafMissingTarget(t *testing.T) {
	root := Node(&Leaf{Pane: "editor"})
	out, ok := SplitLeaf(root, "ghost", "new", ZoneRight)
	if ok {
		t.Fatal("split onto a missing target should report ok=false")
	}
	if out.(*Leaf).Pane != "editor" {
		t.Fatalf("tree mutated on failed split: %#v", out)
	}
}

// TestCloseCollapsesSibling verifies Close removes a leaf and the sibling takes
// the parent split's place.
func TestCloseCollapsesSibling(t *testing.T) {
	root := Node(&Split{Orient: Horizontal, Ratio: 0.5, A: &Leaf{Pane: "explorer"}, B: &Leaf{Pane: "editor"}})
	out, ok := Close(root, "editor")
	if !ok {
		t.Fatal("Close should succeed")
	}
	leaf, isLeaf := out.(*Leaf)
	if !isLeaf || leaf.Pane != "explorer" {
		t.Fatalf("sibling did not collapse up: %#v", out)
	}
}

// TestCloseLastLeafNoOp guards the never-empty invariant: closing the only leaf
// is refused.
func TestCloseLastLeafNoOp(t *testing.T) {
	root := Node(&Leaf{Pane: "explorer"})
	out, ok := Close(root, "explorer")
	if ok {
		t.Fatal("closing the only leaf must be a no-op")
	}
	if out.(*Leaf).Pane != "explorer" {
		t.Fatalf("tree mutated on last-leaf close: %#v", out)
	}
}

// TestSplitThenCloseRoundTrips builds a three-pane tree by splitting twice, then
// closes back down, confirming Split and Close are structural inverses.
func TestSplitThenCloseRoundTrips(t *testing.T) {
	root := Node(&Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{Pane: "explorer"}, B: &Leaf{Pane: "editor"}})
	root, ok := SplitLeaf(root, "editor", "editor:2", ZoneRight)
	if !ok {
		t.Fatal("first split failed")
	}
	if got := Leaves(root); len(got) != 3 {
		t.Fatalf("want 3 leaves, got %v", got)
	}
	root, ok = Close(root, "editor:2")
	if !ok {
		t.Fatal("close failed")
	}
	got := Leaves(root)
	if len(got) != 2 || got[0] != "explorer" || got[1] != "editor" {
		t.Fatalf("did not round-trip to the original pair: %v", got)
	}
}

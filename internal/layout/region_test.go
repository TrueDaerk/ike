package layout

import "testing"

// region_test.go covers the in-tree position helpers (#2191): the ancestor
// path of a leaf and the edge leaf of a region.

// nestedTree is the layout the run-output placement bug lived in: an explorer
// column beside an editor column whose own bottom holds the tool strip.
//
//	H{ explorer , V{ editor , problems } }
func nestedTree() Node {
	return &Split{Orient: Horizontal, Ratio: 0.2,
		A: &Leaf{Pane: "explorer"},
		B: &Split{Orient: Vertical, Ratio: 0.7,
			A: &Leaf{Pane: "editor"},
			B: &Leaf{Pane: "problems"},
		},
	}
}

func TestHopsInnermostFirst(t *testing.T) {
	hops := Hops(nestedTree(), "editor")
	if len(hops) != 2 {
		t.Fatalf("hops = %d, want 2 (the tool strip, then the explorer column)", len(hops))
	}
	if hops[0].Zone != ZoneBottom {
		t.Errorf("first hop zone = %v, want ZoneBottom (problems sits below the editor)", hops[0].Zone)
	}
	if l, ok := hops[0].Sibling.(*Leaf); !ok || l.Pane != "problems" {
		t.Errorf("first sibling = %#v, want the problems leaf", hops[0].Sibling)
	}
	if hops[0].Ratio != 0.7 {
		t.Errorf("first hop ratio = %v, want the split's 0.7", hops[0].Ratio)
	}
	if hops[1].Zone != ZoneLeft {
		t.Errorf("second hop zone = %v, want ZoneLeft (the explorer column)", hops[1].Zone)
	}
	if l, ok := hops[1].Sibling.(*Leaf); !ok || l.Pane != "explorer" {
		t.Errorf("second sibling = %#v, want the explorer leaf", hops[1].Sibling)
	}
}

func TestHopsMissingLeaf(t *testing.T) {
	if hops := Hops(nestedTree(), "nope"); hops != nil {
		t.Errorf("hops for an unknown leaf = %+v, want none", hops)
	}
	if hops := Hops(&Leaf{Pane: "editor"}, "editor"); hops != nil {
		t.Errorf("hops of a bare-leaf root = %+v, want none", hops)
	}
	if hops := Hops(nestedTree(), ""); hops != nil {
		t.Errorf("hops for an empty id = %+v, want none", hops)
	}
}

// TestEdgeLeafInDescends: a region's edge leaf is found even across a split
// running perpendicular to the dock axis, where EdgeLeaf gives up.
func TestEdgeLeafInDescends(t *testing.T) {
	strip := &Split{Orient: Horizontal, Ratio: 0.5,
		A: &Leaf{Pane: "problems"},
		B: &Leaf{Pane: "tests"},
	}
	if got := EdgeLeaf(strip, ZoneBottom); got != "" {
		t.Fatalf("EdgeLeaf on a subdivided strip = %q, want the empty slot", got)
	}
	if got := EdgeLeafIn(strip, ZoneBottom); got != "tests" {
		t.Errorf("EdgeLeafIn(bottom) = %q, want the strip's B side", got)
	}
	if got := EdgeLeafIn(strip, ZoneTop); got != "problems" {
		t.Errorf("EdgeLeafIn(top) = %q, want the strip's A side", got)
	}
	if got := EdgeLeafIn(&Leaf{Pane: "solo"}, ZoneRight); got != "solo" {
		t.Errorf("EdgeLeafIn of a bare leaf = %q, want the leaf itself", got)
	}
	if got := EdgeLeafIn(nil, ZoneRight); got != "" {
		t.Errorf("EdgeLeafIn(nil) = %q, want the empty string", got)
	}
}

func TestOpposite(t *testing.T) {
	for zone, want := range map[Zone]Zone{
		ZoneLeft:   ZoneRight,
		ZoneRight:  ZoneLeft,
		ZoneTop:    ZoneBottom,
		ZoneBottom: ZoneTop,
		ZoneCenter: ZoneCenter,
	} {
		if got := Opposite(zone); got != want {
			t.Errorf("Opposite(%v) = %v, want %v", zone, got, want)
		}
	}
}

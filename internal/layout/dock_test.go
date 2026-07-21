package layout

import "testing"

// three builds explorer | (editor / terminal) — the canonical nested tree.
func three() Node {
	return &Split{Orient: Horizontal, Ratio: 0.3,
		A: &Leaf{"explorer"},
		B: &Split{Orient: Vertical, Ratio: 0.5, A: &Leaf{"editor"}, B: &Leaf{"terminal"}},
	}
}

func leaves(n Node) map[string]bool {
	out := map[string]bool{}
	for _, l := range Leaves(n) {
		out[l] = true
	}
	return out
}

// TestDockEdges guards #811: docking re-roots the tree so the pane spans the
// full dimension of the docked edge, keeping every pane.
func TestDockEdges(t *testing.T) {
	cases := []struct {
		zone   Zone
		orient Orient
		first  bool // docked leaf is child A
	}{
		{ZoneTop, Vertical, true},
		{ZoneBottom, Vertical, false},
		{ZoneLeft, Horizontal, true},
		{ZoneRight, Horizontal, false},
	}
	for _, c := range cases {
		out := Dock(three(), "terminal", c.zone, 0.3)
		s, ok := out.(*Split)
		if !ok || s.Orient != c.orient {
			t.Fatalf("zone %v: root = %#v, want %v split", c.zone, out, c.orient)
		}
		docked := s.B
		if c.first {
			docked = s.A
		}
		if l, ok := docked.(*Leaf); !ok || l.Pane != "terminal" {
			t.Fatalf("zone %v: docked child = %#v, want terminal leaf", c.zone, docked)
		}
		if got := leaves(out); len(got) != 3 || !got["explorer"] || !got["editor"] || !got["terminal"] {
			t.Fatalf("zone %v: pane set changed: %v", c.zone, got)
		}
		// The docked pane spans the full dimension of its edge.
		lay := Compute(out, Rect{X: 0, Y: 0, W: 100, H: 40})
		r := lay.Panes["terminal"]
		if c.orient == Vertical && r.W != 100 {
			t.Fatalf("zone %v: docked width = %d, want full 100", c.zone, r.W)
		}
		if c.orient == Horizontal && r.H != 40 {
			t.Fatalf("zone %v: docked height = %d, want full 40", c.zone, r.H)
		}
	}
}

// TestDockRatioClamped: extreme ratios clamp instead of producing a
// degenerate split.
func TestDockRatioClamped(t *testing.T) {
	for _, ratio := range []float64{-1, 0, 0.05, 0.95, 2} {
		out := Dock(three(), "editor", ZoneTop, ratio)
		s, ok := out.(*Split)
		if !ok || s.Ratio < 0.1 || s.Ratio > 0.9 {
			t.Fatalf("ratio %v: root ratio = %v, want clamped into [0.1, 0.9]", ratio, out)
		}
	}
}

// TestDockNoops: the only leaf, an unknown pane, and an invalid zone leave
// the tree unchanged (and unmutated).
func TestDockNoops(t *testing.T) {
	lone := &Leaf{"editor"}
	if out := Dock(lone, "editor", ZoneTop, 0.3); out != lone {
		t.Fatalf("docking the only leaf must be a no-op, got %#v", out)
	}
	tree := three()
	if out := Dock(tree, "ghost", ZoneTop, 0.3); out != tree {
		t.Fatal("unknown pane must be a no-op")
	}
	tree = three()
	if out := Dock(tree, "terminal", ZoneCenter, 0.3); out != tree {
		t.Fatal("invalid zone must be a no-op")
	}
	if got := leaves(tree); len(got) != 3 {
		t.Fatalf("invalid zone mutated the tree: %v", got)
	}
}

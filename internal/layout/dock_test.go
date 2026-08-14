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

// TestDockNewEdges guards #1889: DockNew attaches a brand-new leaf full-span
// against the named edge, like Dock but without removing anything first.
func TestDockNewEdges(t *testing.T) {
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
		out := DockNew(three(), "tool", c.zone, 0.3)
		s, ok := out.(*Split)
		if !ok || s.Orient != c.orient {
			t.Fatalf("zone %v: root = %#v, want %v split", c.zone, out, c.orient)
		}
		docked := s.B
		if c.first {
			docked = s.A
		}
		if l, ok := docked.(*Leaf); !ok || l.Pane != "tool" {
			t.Fatalf("zone %v: docked child = %#v, want tool leaf", c.zone, docked)
		}
		if got := leaves(out); len(got) != 4 || !got["tool"] {
			t.Fatalf("zone %v: pane set = %v, want the three plus tool", c.zone, got)
		}
		lay := Compute(out, Rect{X: 0, Y: 0, W: 100, H: 40})
		r := lay.Panes["tool"]
		if c.orient == Vertical && r.W != 100 {
			t.Fatalf("zone %v: docked width = %d, want full 100", c.zone, r.W)
		}
		if c.orient == Horizontal && r.H != 40 {
			t.Fatalf("zone %v: docked height = %d, want full 40", c.zone, r.H)
		}
	}
}

// TestDockNewRejects: an empty pane id or a non-edge zone returns the tree
// unchanged.
func TestDockNewRejects(t *testing.T) {
	tree := three()
	if out := DockNew(tree, "", ZoneBottom, 0.3); out != tree {
		t.Fatal("empty pane id must be a no-op")
	}
	if out := DockNew(tree, "tool", ZoneCenter, 0.3); out != tree {
		t.Fatal("ZoneCenter must be a no-op")
	}
}

// TestEdgeLeaf guards #1889's occupancy probe: only a lone leaf pinned
// against the workspace edge counts as occupying that dock slot.
func TestEdgeLeaf(t *testing.T) {
	tree := three() // explorer | (editor / terminal)
	if got := EdgeLeaf(tree, ZoneLeft); got != "explorer" {
		t.Fatalf("left occupant = %q, want explorer", got)
	}
	// The right edge is the editor/terminal column, not a lone leaf.
	if got := EdgeLeaf(tree, ZoneRight); got != "" {
		t.Fatalf("right occupant = %q, want none (subdivided edge)", got)
	}
	// Top/bottom edges are shared by both columns of the horizontal root.
	if got := EdgeLeaf(tree, ZoneBottom); got != "" {
		t.Fatalf("bottom occupant = %q, want none (shared edge)", got)
	}
	// A bare-leaf root occupies nothing — one pane just fills the workspace.
	if got := EdgeLeaf(&Leaf{"editor"}, ZoneBottom); got != "" {
		t.Fatalf("bare leaf occupant = %q, want none", got)
	}
	// A docked strip is found through nested same-orientation splits.
	docked := DockNew(three(), "tool", ZoneBottom, 0.3)
	if got := EdgeLeaf(docked, ZoneBottom); got != "tool" {
		t.Fatalf("bottom occupant after dock = %q, want tool", got)
	}
	stacked := DockNew(docked, "deeper", ZoneBottom, 0.3)
	if got := EdgeLeaf(stacked, ZoneBottom); got != "deeper" {
		t.Fatalf("nested bottom occupant = %q, want deeper", got)
	}
	if got := EdgeLeaf(docked, ZoneTop); got != "" {
		t.Fatalf("top occupant = %q, want none", got)
	}
}

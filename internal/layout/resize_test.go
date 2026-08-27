package layout

import "testing"

// TestResizeStepAccumulates (#2150): repeated single-cell steps move the edge
// one cell each — the ratio round-trip must not lose them.
func TestResizeStepAccumulates(t *testing.T) {
	sp := &Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"explorer"}, B: &Leaf{"editor"}}
	vp := Rect{0, 0, 100, 40}
	for i := 1; i <= 5; i++ {
		Compute(sp, vp).Dividers[0].ResizeStep(1)
		if w := Compute(sp, vp).Panes["explorer"].W; w != 30+i {
			t.Fatalf("step %d: explorer width = %d, want %d", i, w, 30+i)
		}
	}
	// Negative steps walk back the same way.
	Compute(sp, vp).Dividers[0].ResizeStep(-5)
	if w := Compute(sp, vp).Panes["explorer"].W; w != 30 {
		t.Fatalf("after -5 steps explorer width = %d, want 30", w)
	}
}

// TestResizeStepClamps (#2150): a step can never push a child below the
// minimum, however often it is repeated.
func TestResizeStepClamps(t *testing.T) {
	sp := &Split{Orient: Vertical, Ratio: 0.5, A: &Leaf{"top"}, B: &Leaf{"bottom"}}
	vp := Rect{0, 0, 40, 20}
	for i := 0; i < 50; i++ {
		Compute(sp, vp).Dividers[0].ResizeStep(1)
	}
	l := Compute(sp, vp)
	if l.Panes["bottom"].H < minCell {
		t.Fatalf("bottom pane shrank to %d, below minCell %d", l.Panes["bottom"].H, minCell)
	}
	for i := 0; i < 50; i++ {
		Compute(sp, vp).Dividers[0].ResizeStep(-1)
	}
	l = Compute(sp, vp)
	if l.Panes["top"].H < minCell {
		t.Fatalf("top pane shrank to %d, below minCell %d", l.Panes["top"].H, minCell)
	}
}

// TestResizeStepTinySpan: a span too small for both minimums leaves the ratio
// alone, like ResizeTo.
func TestResizeStepTinySpan(t *testing.T) {
	sp := &Split{Orient: Horizontal, Ratio: 0.5, A: &Leaf{"a"}, B: &Leaf{"b"}}
	vp := Rect{0, 0, 6, 10}
	Compute(sp, vp).Dividers[0].ResizeStep(1)
	if sp.Ratio != 0.5 {
		t.Fatalf("ratio = %v, want it unchanged at 0.5", sp.Ratio)
	}
}

// TestEdgeDividerPicksTrailingEdge (#2150): the pane that owns its right edge
// grows when the edge moves right; the pane on the other side of the same
// split only owns its left edge and shrinks instead.
func TestEdgeDividerPicksTrailingEdge(t *testing.T) {
	sp := &Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"explorer"}, B: &Leaf{"editor"}}
	vp := Rect{0, 0, 100, 40}
	l := Compute(sp, vp)

	d, ok := l.EdgeDivider("explorer", ZoneRight)
	if !ok || d.Split != sp {
		t.Fatalf("explorer right edge: %+v ok=%v, want the root split", d, ok)
	}
	d.ResizeStep(1)
	if w := Compute(sp, vp).Panes["explorer"].W; w != 31 {
		t.Fatalf("explorer width = %d, want 31 (grown)", w)
	}

	// The editor has no divider on its right edge (the viewport ends there),
	// so its left edge answers — moving it right shrinks the editor.
	l = Compute(sp, vp)
	d, ok = l.EdgeDivider("editor", ZoneRight)
	if !ok || d.Split != sp {
		t.Fatalf("editor right edge: %+v ok=%v, want the root split", d, ok)
	}
	d.ResizeStep(1)
	if w := Compute(sp, vp).Panes["editor"].W; w != 68 {
		t.Fatalf("editor width = %d, want 68 (shrunk)", w)
	}
}

// TestEdgeDividerNearestAncestor: with nested splits the pane's own enclosing
// split answers, not the outer one that happens to share the coordinate.
func TestEdgeDividerNearestAncestor(t *testing.T) {
	inner := &Split{Orient: Vertical, Ratio: 0.5, A: &Leaf{"top"}, B: &Leaf{"bottom"}}
	root := &Split{Orient: Horizontal, Ratio: 0.5, A: &Leaf{"left"}, B: inner}
	vp := Rect{0, 0, 100, 40}
	l := Compute(root, vp)

	d, ok := l.EdgeDivider("top", ZoneBottom)
	if !ok || d.Split != inner {
		t.Fatalf("top bottom edge resolved to %+v ok=%v, want the inner split", d.Split, ok)
	}
	d.ResizeStep(2)
	if h := Compute(root, vp).Panes["top"].H; h != 22 {
		t.Fatalf("top height = %d, want 22", h)
	}
	// A direction with no split on either edge reports nothing.
	if _, ok := Compute(root, vp).EdgeDivider("left", ZoneBottom); ok {
		t.Fatal("left pane spans the full height: no vertical divider to move")
	}
}

// TestEdgeDividerUnknownPane: an unknown key never returns a divider.
func TestEdgeDividerUnknownPane(t *testing.T) {
	l := Compute(&Leaf{"only"}, Rect{0, 0, 40, 20})
	if _, ok := l.EdgeDivider("ghost", ZoneRight); ok {
		t.Fatal("unknown pane must not resolve a divider")
	}
	if _, ok := l.EdgeDivider("only", ZoneRight); ok {
		t.Fatal("a single pane has no divider to move")
	}
}

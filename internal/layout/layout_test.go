package layout

import "testing"

// tilesExactly asserts the leaf rects cover vp with no gap or overlap by summing
// their areas and checking bounds.
func tilesExactly(t *testing.T, root Node, vp Rect) Layout {
	t.Helper()
	l := Compute(root, vp)
	cover := map[[2]int]int{}
	for _, r := range l.Panes {
		for x := r.X; x < r.X+r.W; x++ {
			for y := r.Y; y < r.Y+r.H; y++ {
				cover[[2]int{x, y}]++
			}
		}
	}
	for _, d := range l.Dividers {
		for x := d.Rect.X; x < d.Rect.X+d.Rect.W; x++ {
			for y := d.Rect.Y; y < d.Rect.Y+d.Rect.H; y++ {
				cover[[2]int{x, y}]++
			}
		}
	}
	for x := vp.X; x < vp.X+vp.W; x++ {
		for y := vp.Y; y < vp.Y+vp.H; y++ {
			if cover[[2]int{x, y}] != 1 {
				t.Fatalf("cell (%d,%d) covered %d times, want 1", x, y, cover[[2]int{x, y}])
			}
		}
	}
	return l
}

func TestComputeExactTilingHorizontal(t *testing.T) {
	root := &Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"explorer"}, B: &Leaf{"editor"}}
	vp := Rect{0, 0, 100, 40}
	l := tilesExactly(t, root, vp)
	exp := l.Panes["explorer"]
	if exp.X != 0 || exp.Y != 0 || exp.H != 40 {
		t.Fatalf("explorer rect wrong: %+v", exp)
	}
	if exp.W != 29 { // round(0.3*99)=30? round(29.7)=30 -> wait usable=99
		// usable = 99, round(0.3*99)=round(29.7)=30
		if exp.W != 30 {
			t.Fatalf("explorer width %d", exp.W)
		}
	}
}

func TestComputeExactTilingVertical(t *testing.T) {
	root := &Split{Orient: Vertical, Ratio: 0.5, A: &Leaf{"a"}, B: &Leaf{"b"}}
	tilesExactly(t, root, Rect{0, 0, 50, 21})
}

func TestComputeNested(t *testing.T) {
	root := &Split{Orient: Horizontal, Ratio: 0.5,
		A: &Leaf{"explorer"},
		B: &Split{Orient: Vertical, Ratio: 0.5, A: &Leaf{"editor"}, B: &Leaf{"terminal"}},
	}
	l := tilesExactly(t, root, Rect{0, 0, 80, 30})
	if len(l.Panes) != 3 {
		t.Fatalf("want 3 panes, got %d", len(l.Panes))
	}
}

func TestDefaultLayout(t *testing.T) {
	root := Default(100, 30)
	l := Compute(root, Rect{0, 0, 100, 40})
	if l.Panes["explorer"].W < 25 || l.Panes["explorer"].W > 32 {
		t.Fatalf("default explorer width off: %d", l.Panes["explorer"].W)
	}
}

func TestHitDividerTitlePane(t *testing.T) {
	root := &Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"explorer"}, B: &Leaf{"editor"}}
	l := Compute(root, Rect{0, 0, 100, 40})
	div := l.Dividers[0].Rect
	if h := l.Hit(div.X, 5); h.Kind != HitDivider {
		t.Fatalf("expected divider hit, got %v", h.Kind)
	}
	if h := l.Hit(2, 0); h.Kind != HitTitle || h.Pane != "explorer" {
		t.Fatalf("expected explorer title (border row), got %+v", h)
	}
	// The visible title text sits one row below the top border; grabbing it must
	// also start a move.
	if h := l.Hit(2, 1); h.Kind != HitTitle || h.Pane != "explorer" {
		t.Fatalf("expected explorer title (text row), got %+v", h)
	}
	if h := l.Hit(2, 5); h.Kind != HitPane || h.Pane != "explorer" {
		t.Fatalf("expected explorer pane, got %+v", h)
	}
}

func TestResizeClamp(t *testing.T) {
	s := &Split{Orient: Horizontal, Ratio: 0.5, A: &Leaf{"a"}, B: &Leaf{"b"}}
	l := Compute(s, Rect{0, 0, 100, 40})
	d := l.Dividers[0]
	d.ResizeTo(1, 5) // far left, below min
	if got := Compute(s, Rect{0, 0, 100, 40}).Panes["a"].W; got < minCell {
		t.Fatalf("left pane collapsed to %d", got)
	}
	d.ResizeTo(99, 5) // far right, below min for b
	if got := Compute(s, Rect{0, 0, 100, 40}).Panes["b"].W; got < minCell {
		t.Fatalf("right pane collapsed to %d", got)
	}
}

func TestResizeMidpoint(t *testing.T) {
	s := &Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"a"}, B: &Leaf{"b"}}
	l := Compute(s, Rect{0, 0, 100, 40})
	l.Dividers[0].ResizeTo(50, 5)
	if r := s.Ratio; r < 0.49 || r > 0.51 {
		t.Fatalf("ratio after drag to 50 = %f", r)
	}
}

func TestMoveSwap(t *testing.T) {
	root := Node(&Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"explorer"}, B: &Leaf{"editor"}})
	root = Move(root, "explorer", "editor", ZoneRight)
	s := root.(*Split)
	if s.Orient != Horizontal {
		t.Fatalf("orient changed: %v", s.Orient)
	}
	if s.A.(*Leaf).Pane != "editor" || s.B.(*Leaf).Pane != "explorer" {
		t.Fatalf("panes not reordered: %+v %+v", s.A, s.B)
	}
}

func TestMoveReorient(t *testing.T) {
	root := Node(&Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"explorer"}, B: &Leaf{"editor"}})
	root = Move(root, "explorer", "editor", ZoneBottom)
	s := root.(*Split)
	if s.Orient != Vertical {
		t.Fatalf("expected vertical, got %v", s.Orient)
	}
	if s.A.(*Leaf).Pane != "editor" || s.B.(*Leaf).Pane != "explorer" {
		t.Fatalf("wrong stack order: %+v %+v", s.A, s.B)
	}
}

func TestMovePreservesPaneSet(t *testing.T) {
	root := Node(&Split{Orient: Horizontal, Ratio: 0.5,
		A: &Leaf{"explorer"},
		B: &Split{Orient: Vertical, Ratio: 0.5, A: &Leaf{"editor"}, B: &Leaf{"terminal"}},
	})
	root = Move(root, "terminal", "explorer", ZoneLeft)
	got := Panes(root)
	for _, want := range []string{"explorer", "editor", "terminal"} {
		if !got[want] {
			t.Fatalf("pane %q lost after move", want)
		}
	}
	if len(got) != 3 {
		t.Fatalf("pane count changed: %d", len(got))
	}
}

func TestMoveNoopSelf(t *testing.T) {
	root := Node(&Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"explorer"}, B: &Leaf{"editor"}})
	out := Move(root, "explorer", "explorer", ZoneRight)
	if out != root {
		t.Fatal("self-move should be a no-op")
	}
}

func TestDropZone(t *testing.T) {
	r := Rect{0, 0, 100, 40}
	cases := []struct {
		x, y int
		want Zone
	}{
		{2, 20, ZoneLeft},
		{98, 20, ZoneRight},
		{50, 1, ZoneTop},
		{50, 38, ZoneBottom},
	}
	for _, c := range cases {
		if got := DropZone(r, c.x, c.y); got != c.want {
			t.Fatalf("DropZone(%d,%d)=%v want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestStateRoundTrip(t *testing.T) {
	root := Node(&Split{Orient: Horizontal, Ratio: 0.3,
		A: &Leaf{"explorer"},
		B: &Split{Orient: Vertical, Ratio: 0.4, A: &Leaf{"editor"}, B: &Leaf{"terminal"}},
	})
	data, err := Encode(root)
	if err != nil {
		t.Fatal(err)
	}
	valid := map[string]bool{"explorer": true, "editor": true, "terminal": true}
	got, ok := Decode(data, valid)
	if !ok {
		t.Fatal("round-trip decode failed")
	}
	if !equal(root, got) {
		t.Fatalf("decoded tree differs")
	}
}

func TestDecodeTolerant(t *testing.T) {
	valid := map[string]bool{"explorer": true, "editor": true}
	// unknown pane id
	data, _ := Encode(&Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"explorer"}, B: &Leaf{"ghost"}})
	if _, ok := Decode(data, valid); ok {
		t.Fatal("unknown pane id should be rejected")
	}
	// missing a pane
	data, _ = Encode(&Leaf{"explorer"})
	if _, ok := Decode(data, valid); ok {
		t.Fatal("missing pane should be rejected")
	}
	// duplicate pane
	data, _ = Encode(&Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"explorer"}, B: &Leaf{"explorer"}})
	if _, ok := Decode(data, valid); ok {
		t.Fatal("duplicate pane should be rejected")
	}
	// garbage
	if _, ok := Decode([]byte("not json"), valid); ok {
		t.Fatal("garbage should be rejected")
	}
}

func equal(a, b Node) bool {
	switch ta := a.(type) {
	case *Leaf:
		tb, ok := b.(*Leaf)
		return ok && ta.Pane == tb.Pane
	case *Split:
		tb, ok := b.(*Split)
		return ok && ta.Orient == tb.Orient && equal(ta.A, tb.A) && equal(ta.B, tb.B)
	}
	return false
}

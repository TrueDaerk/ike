package layout

import "testing"

// tilesExactly asserts the leaf rects alone cover vp with no gap or overlap
// (#761: dividers are hit bands over pane borders, not reserved cells) and that
// every divider band lies inside vp.
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
	for x := vp.X; x < vp.X+vp.W; x++ {
		for y := vp.Y; y < vp.Y+vp.H; y++ {
			if cover[[2]int{x, y}] != 1 {
				t.Fatalf("cell (%d,%d) covered %d times, want 1", x, y, cover[[2]int{x, y}])
			}
		}
	}
	for _, d := range l.Dividers {
		r := d.Rect
		if r.X < vp.X || r.Y < vp.Y || r.X+r.W > vp.X+vp.W || r.Y+r.H > vp.Y+vp.H {
			t.Fatalf("divider band %+v escapes viewport %+v", r, vp)
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

// The resize band covers the two border cells at the children's shared edge:
// A's right border column and B's left border column (#761).
func TestEdgeBandCoversAdjacentBorders(t *testing.T) {
	root := &Split{Orient: Horizontal, Ratio: 0.3, A: &Leaf{"explorer"}, B: &Leaf{"editor"}}
	l := Compute(root, Rect{0, 0, 100, 40})
	a, b := l.Panes["explorer"], l.Panes["editor"]
	band := l.Dividers[0].Rect
	if band.X != a.X+a.W-1 || band.W != 2 || band.X+1 != b.X {
		t.Fatalf("band %+v does not straddle boundary between %+v and %+v", band, a, b)
	}
	// Both border columns start a resize, even on the border rows of the panes.
	for _, x := range []int{band.X, band.X + 1} {
		if h := l.Hit(x, 5); h.Kind != HitDivider {
			t.Fatalf("Hit(%d,5)=%v, want HitDivider", x, h.Kind)
		}
		if h := l.Hit(x, 0); h.Kind != HitDivider {
			t.Fatalf("Hit(%d,0)=%v, want HitDivider (band beats title band)", x, h.Kind)
		}
	}
	// One cell beyond the band on either side is pane territory again.
	if h := l.Hit(band.X-1, 5); h.Kind != HitPane || h.Pane != "explorer" {
		t.Fatalf("left of band: %+v", h)
	}
	if h := l.Hit(band.X+2, 5); h.Kind != HitPane || h.Pane != "editor" {
		t.Fatalf("right of band: %+v", h)
	}
}

// Vertical splits get the same band, over A's bottom and B's top border rows.
func TestEdgeBandVertical(t *testing.T) {
	root := &Split{Orient: Vertical, Ratio: 0.5, A: &Leaf{"a"}, B: &Leaf{"b"}}
	l := Compute(root, Rect{0, 0, 50, 21})
	a, b := l.Panes["a"], l.Panes["b"]
	band := l.Dividers[0].Rect
	if band.Y != a.Y+a.H-1 || band.H != 2 || band.Y+1 != b.Y {
		t.Fatalf("band %+v does not straddle boundary between %+v and %+v", band, a, b)
	}
	if h := l.Hit(10, band.Y); h.Kind != HitDivider {
		t.Fatalf("Hit on A's bottom border: %v", h.Kind)
	}
	if h := l.Hit(10, band.Y+1); h.Kind != HitDivider {
		t.Fatalf("Hit on B's top border: %v", h.Kind)
	}
	// B's title text row (one below its top border) still starts a move.
	if h := l.Hit(10, b.Y+1); h.Kind != HitTitle || h.Pane != "b" {
		t.Fatalf("B title row: %+v", h)
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

func TestDropZoneWithCenter(t *testing.T) {
	r := Rect{0, 0, 100, 40}
	cases := []struct {
		x, y int
		want Zone
	}{
		{2, 20, ZoneLeft},
		{98, 20, ZoneRight},
		{50, 1, ZoneTop},
		{50, 38, ZoneBottom},
		{50, 20, ZoneCenter}, // dead center
		{35, 15, ZoneCenter}, // just inside the interior band on both axes
		{2, 2, ZoneLeft},     // corner stays an edge, never center
		{20, 20, ZoneLeft},   // inside the horizontal band → edge wins
		{50, 10, ZoneTop},    // inside the vertical band → edge wins
	}
	for _, c := range cases {
		if got := DropZoneWithCenter(r, c.x, c.y); got != c.want {
			t.Fatalf("DropZoneWithCenter(%d,%d)=%v want %v", c.x, c.y, got, c.want)
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

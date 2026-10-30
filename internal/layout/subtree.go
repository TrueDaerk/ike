package layout

// subtree.go operates on whole subtrees rather than single leaves (#3109):
// the saved-layout editor anchor re-creates a slot *at the level of the split
// it belonged to*, which means splitting the sibling subtree that stands in
// the slot's place — never just its first leaf.

// CommonAncestor returns the smallest subtree of root holding every leaf
// whose pane id is in panes, nil when panes is empty or none of them is in
// the tree. Leaves absent from the tree are ignored.
func CommonAncestor(root Node, panes map[string]bool) Node {
	if root == nil || len(panes) == 0 {
		return nil
	}
	var find func(Node) (Node, int)
	find = func(n Node) (Node, int) {
		switch t := n.(type) {
		case *Leaf:
			if panes[t.Pane] {
				return t, 1
			}
			return nil, 0
		case *Split:
			a, na := find(t.A)
			b, nb := find(t.B)
			switch {
			case na > 0 && nb > 0:
				return t, na + nb
			case na > 0:
				return a, na
			case nb > 0:
				return b, nb
			}
		}
		return nil, 0
	}
	n, _ := find(root)
	return n
}

// SplitNode wraps the subtree at (matched by identity) in a fresh split
// pairing it with a brand-new Leaf{Pane: newPane} on zone's side — the
// subtree counterpart of SplitLeaf. The new split starts at an even ratio;
// callers adjust it. A nil or foreign at, or an empty newPane, returns root
// unchanged with ok=false.
func SplitNode(root Node, at Node, newPane string, zone Zone) (Node, bool) {
	if root == nil || at == nil || newPane == "" {
		return root, false
	}
	wrap := func(n Node) *Split {
		fresh := &Leaf{Pane: newPane}
		s := &Split{Orient: Horizontal, Ratio: 0.5}
		if zone == ZoneTop || zone == ZoneBottom {
			s.Orient = Vertical
		}
		if zone == ZoneLeft || zone == ZoneTop {
			s.A, s.B = fresh, n
		} else {
			s.A, s.B = n, fresh
		}
		return s
	}
	if root == at {
		return wrap(root), true
	}
	found := false
	var walk func(Node)
	walk = func(n Node) {
		s, ok := n.(*Split)
		if !ok || found {
			return
		}
		if s.A == at {
			s.A = wrap(at)
			found = true
			return
		}
		if s.B == at {
			s.B = wrap(at)
			found = true
			return
		}
		walk(s.A)
		walk(s.B)
	}
	walk(root)
	return root, found
}

// Bounds returns the rectangle a subtree occupies, given the leaf rectangles
// Compute/Rects assigned over the viewport: the bounding box of its leaves,
// which tile it exactly. ok=false for a subtree none of whose leaves has a
// rectangle.
func Bounds(n Node, rects map[string]Rect) (Rect, bool) {
	var out Rect
	ok := false
	for _, key := range Leaves(n) {
		r, has := rects[key]
		if !has {
			continue
		}
		if !ok {
			out, ok = r, true
			continue
		}
		x0, y0 := minInt(out.X, r.X), minInt(out.Y, r.Y)
		x1, y1 := maxInt(out.X+out.W, r.X+r.W), maxInt(out.Y+out.H, r.Y+r.H)
		out = Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
	}
	return out, ok
}

// Overlap returns the area (in cells) shared by a and b.
func Overlap(a, b Rect) int {
	w := minInt(a.X+a.W, b.X+b.W) - maxInt(a.X, b.X)
	h := minInt(a.Y+a.H, b.Y+b.H) - maxInt(a.Y, b.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

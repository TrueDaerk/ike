package layout

// Move relocates the leaf src onto target's drop zone, re-parenting it without
// changing the pane set. It first removes src (its parent split collapses, the
// sibling taking the parent's place), then re-inserts src beside target in a new
// split whose orientation and child order follow zone. src == target, a missing
// pane, or a removal that would empty the tree returns root unchanged.
func Move(root Node, src, target string, zone Zone) Node {
	if src == target || src == "" || target == "" {
		return root
	}
	pruned, leaf, ok := remove(root, src)
	if !ok || leaf == nil || pruned == nil {
		return root
	}
	out, ok := insert(pruned, target, leaf, zone)
	if !ok {
		return root
	}
	return out
}

// Dock detaches the leaf src and re-attaches it against the tree's outer
// edge (#811), splitting at the root: ZoneTop/ZoneBottom make it span the
// full workspace width, ZoneLeft/ZoneRight the full height. ratio is the
// docked pane's share of the workspace along the dock axis, clamped to
// [0.1, 0.9]. Docking the only leaf or an unknown pane returns root
// unchanged.
func Dock(root Node, src string, zone Zone, ratio float64) Node {
	if src == "" {
		return root
	}
	switch zone {
	case ZoneTop, ZoneBottom, ZoneLeft, ZoneRight:
	default:
		return root // remove() mutates in place — reject bad zones up front
	}
	pruned, leaf, ok := remove(root, src)
	if !ok || leaf == nil || pruned == nil {
		return root
	}
	return attachEdge(pruned, leaf, zone, ratio)
}

// DockNew attaches a brand-new leaf against the tree's outer edge — the
// create counterpart of Dock (#1889): a tool pane opening at its configured
// home position docks full-span without existing anywhere in the tree first.
// ratio is the new leaf's share along the dock axis, clamped like Dock's. An
// empty pane id or a non-edge zone returns root unchanged.
func DockNew(root Node, newPane string, zone Zone, ratio float64) Node {
	if newPane == "" {
		return root
	}
	switch zone {
	case ZoneTop, ZoneBottom, ZoneLeft, ZoneRight:
	default:
		return root
	}
	return attachEdge(root, &Leaf{Pane: newPane}, zone, ratio)
}

// attachEdge splits at the root to pin leaf against the zone's edge, leaf
// taking ratio (clamped to [0.1, 0.9]) of the workspace along the dock axis.
func attachEdge(root Node, leaf *Leaf, zone Zone, ratio float64) Node {
	if ratio < 0.1 {
		ratio = 0.1
	}
	if ratio > 0.9 {
		ratio = 0.9
	}
	switch zone {
	case ZoneTop:
		return &Split{Orient: Vertical, Ratio: ratio, A: leaf, B: root}
	case ZoneBottom:
		return &Split{Orient: Vertical, Ratio: 1 - ratio, A: root, B: leaf}
	case ZoneLeft:
		return &Split{Orient: Horizontal, Ratio: ratio, A: leaf, B: root}
	default: // ZoneRight (both callers validate the zone)
		return &Split{Orient: Horizontal, Ratio: 1 - ratio, A: root, B: leaf}
	}
}

// EdgeLeaf returns the pane id occupying the dock slot at the tree's outer
// edge named by zone (#1889): starting at the root, splits oriented along the
// dock axis are followed toward that edge until a lone leaf is pinned against
// it. A bare-leaf root (one pane fills the workspace), a root split of the
// other orientation (the edge is shared by several columns/rows), or a
// subdivided edge strip yields "" — the slot then counts as free.
func EdgeLeaf(root Node, zone Zone) string {
	orient := Horizontal
	if zone == ZoneTop || zone == ZoneBottom {
		orient = Vertical
	}
	first := zone == ZoneTop || zone == ZoneLeft
	for {
		s, ok := root.(*Split)
		if !ok || s.Orient != orient {
			return ""
		}
		side := s.B
		if first {
			side = s.A
		}
		if leaf, ok := side.(*Leaf); ok {
			return leaf.Pane
		}
		root = side
	}
}

// remove detaches the leaf with pane id src, returning the tree with src's
// parent split replaced by src's sibling. Removing the only node (root is the
// leaf) reports ok=false so callers keep the tree intact.
func remove(n Node, src string) (pruned Node, removed *Leaf, ok bool) {
	switch t := n.(type) {
	case *Leaf:
		return n, nil, false // a bare leaf has no parent to collapse into
	case *Split:
		if la, isLeaf := t.A.(*Leaf); isLeaf && la.Pane == src {
			return t.B, la, true
		}
		if lb, isLeaf := t.B.(*Leaf); isLeaf && lb.Pane == src {
			return t.A, lb, true
		}
		if p, r, found := remove(t.A, src); found {
			t.A = p
			return t, r, true
		}
		if p, r, found := remove(t.B, src); found {
			t.B = p
			return t, r, true
		}
	}
	return n, nil, false
}

// insert replaces the leaf with pane id target by a new split pairing the
// existing target leaf with leaf, ordered and oriented per zone.
func insert(n Node, target string, leaf *Leaf, zone Zone) (Node, bool) {
	switch t := n.(type) {
	case *Leaf:
		if t.Pane != target {
			return n, false
		}
		return splitFor(t, leaf, zone), true
	case *Split:
		if out, ok := insert(t.A, target, leaf, zone); ok {
			t.A = out
			return t, true
		}
		if out, ok := insert(t.B, target, leaf, zone); ok {
			t.B = out
			return t, true
		}
	}
	return n, false
}

// splitFor builds the split that pairs the dropped leaf with the target leaf at
// an even ratio, placing the dropped leaf on the side named by zone.
func splitFor(target, dropped *Leaf, zone Zone) *Split {
	switch zone {
	case ZoneLeft:
		return &Split{Orient: Horizontal, Ratio: 0.5, A: dropped, B: target}
	case ZoneRight:
		return &Split{Orient: Horizontal, Ratio: 0.5, A: target, B: dropped}
	case ZoneTop:
		return &Split{Orient: Vertical, Ratio: 0.5, A: dropped, B: target}
	default: // ZoneBottom
		return &Split{Orient: Vertical, Ratio: 0.5, A: target, B: dropped}
	}
}

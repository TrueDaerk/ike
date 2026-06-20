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

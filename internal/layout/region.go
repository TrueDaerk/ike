package layout

// region.go answers positional questions *inside* the tree, where move.go's
// EdgeLeaf/DockNew only look at the workspace's outer edges (#2191). A nested
// layout keeps its tool strip in a region — the bottom of the editor column,
// not the bottom of everything — so the dock rules need the leaf's own
// neighbourhood: Hops walks a leaf's ancestor path outwards, and EdgeLeafIn
// names the leaf pinned against a region's edge.

// Opposite mirrors a zone across the pane it describes: the side a pane sits
// on relative to its neighbour is the opposite of the side the neighbour sits
// on relative to it. ZoneCenter has no opposite and comes back unchanged.
func Opposite(z Zone) Zone {
	switch z {
	case ZoneLeft:
		return ZoneRight
	case ZoneRight:
		return ZoneLeft
	case ZoneTop:
		return ZoneBottom
	case ZoneBottom:
		return ZoneTop
	}
	return z
}

// Hop is one step of a leaf's ancestor path: the subtree hanging off the other
// side of the ancestor split, the side that subtree occupies **relative to the
// leaf**, and the split's ratio.
type Hop struct {
	Sibling Node
	Zone    Zone
	Ratio   float64
}

// Hops returns the ancestor path of the leaf with pane id leaf, innermost
// split first — the nearest neighbourhood before the wider ones, so a caller
// scanning for "what is below me" finds the enclosing region's strip before
// the workspace's. An unknown or empty id, or a bare-leaf root, yields no hops.
func Hops(root Node, leaf string) []Hop {
	if leaf == "" || root == nil {
		return nil
	}
	var out []Hop
	var walk func(Node) bool
	walk = func(n Node) bool {
		switch t := n.(type) {
		case *Leaf:
			return t.Pane == leaf
		case *Split:
			zoneA, zoneB := ZoneLeft, ZoneRight
			if t.Orient == Vertical {
				zoneA, zoneB = ZoneTop, ZoneBottom
			}
			if walk(t.A) {
				// The leaf is in A, so its sibling B sits on the B side.
				out = append(out, Hop{Sibling: t.B, Zone: zoneB, Ratio: t.Ratio})
				return true
			}
			if walk(t.B) {
				out = append(out, Hop{Sibling: t.A, Zone: zoneA, Ratio: t.Ratio})
				return true
			}
		}
		return false
	}
	if !walk(root) {
		return nil
	}
	return out
}

// EdgeLeafIn names the leaf pinned against region n's own zone edge. Unlike
// EdgeLeaf — which probes the workspace edge and gives up as soon as a split
// runs across the dock axis — it always descends to a leaf: a strip already
// subdivided across the axis yields the leaf on zone's side of it (the
// bottom/right-most for ZoneBottom/ZoneRight, the top/left-most otherwise), so
// a caller joining an existing region always has a deterministic neighbour.
// An empty region yields "".
func EdgeLeafIn(n Node, zone Zone) string {
	for {
		s, ok := n.(*Split)
		if !ok {
			break
		}
		if zone == ZoneTop || zone == ZoneLeft {
			n = s.A
		} else {
			n = s.B
		}
	}
	if l, ok := n.(*Leaf); ok {
		return l.Pane
	}
	return ""
}

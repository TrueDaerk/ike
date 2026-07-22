package layout

// SplitLeaf grows the leaf whose id is target into a split pairing the existing
// leaf with a brand-new Leaf{Pane: newPane}, oriented and ordered by zone. It is
// the create half of the pane manager: structurally identical to the second half
// of Move (insert via splitFor), but the inserted leaf is fresh rather than
// removed from elsewhere. A missing target, or target == newPane, returns root
// unchanged with ok=false. (Named SplitLeaf, not Split, because Split is the
// split-node type.)
func SplitLeaf(root Node, target, newPane string, zone Zone) (Node, bool) {
	if target == "" || newPane == "" || target == newPane {
		return root, false
	}
	return insert(root, target, &Leaf{Pane: newPane}, zone)
}

// Replace renames the leaf carrying pane id old to newPane, keeping its exact
// place and geometry in the tree — the swap half of the pane manager, used to
// reuse an empty editor's slot for a diff pane instead of splitting a new one
// (#628). The tree is mutated in place (root is returned for convenience).
// A missing old or an empty old/newPane returns root unchanged with ok=false;
// callers must ensure newPane is not already a leaf elsewhere — Replace does
// not check for collisions.
func Replace(root Node, old, newPane string) (Node, bool) {
	if old == "" || newPane == "" {
		return root, false
	}
	if old == newPane {
		return root, true
	}
	var found bool
	var walk func(Node)
	walk = func(n Node) {
		switch t := n.(type) {
		case *Leaf:
			if t.Pane == old {
				t.Pane = newPane
				found = true
			}
		case *Split:
			walk(t.A)
			walk(t.B)
		}
	}
	walk(root)
	return root, found
}

// Close removes the leaf with id pane, its parent split collapsing so the
// sibling takes the parent's place — the first-class promotion of move.remove.
// Closing the only leaf (root is that leaf) returns root unchanged with
// ok=false, so the workspace never empties; the caller then drops the instance.
func Close(root Node, pane string) (Node, bool) {
	pruned, leaf, ok := remove(root, pane)
	if !ok || leaf == nil {
		return root, false
	}
	return pruned, true
}

// Clone returns a deep copy of the tree. Mutating operations (remove, insert,
// resize) edit splits in place, so callers snapshotting a tree for later
// restore (#791) must copy it first.
func Clone(n Node) Node {
	switch t := n.(type) {
	case *Leaf:
		c := *t
		return &c
	case *Split:
		c := *t
		c.A = Clone(t.A)
		c.B = Clone(t.B)
		return &c
	}
	return nil
}

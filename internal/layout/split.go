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

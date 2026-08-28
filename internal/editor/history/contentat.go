package history

import "ike/internal/editor/buffer"

// ContentAt reconstructs the buffer text of state seq without touching the
// history or the live buffer (#2143): it replays the same inverse/forward walk
// as JumpTo on a scratch copy of cur, so the undo-tree overlay can preview the
// diff of any node against the current state. Returns false for a seq that is
// not in the tree.
func (h *History) ContentAt(cur *buffer.Buffer, seq int) (string, bool) {
	if cur == nil {
		return "", false
	}
	if seq != 0 {
		if _, live := h.nodes[seq]; !live {
			return "", false
		}
	}
	if seq == h.current {
		return cur.String(), true
	}

	// Ancestors of the current state (inclusive), for the LCA lookup. A
	// parent chain broken by pruning would loop forever, so a missing node
	// aborts the preview instead.
	onCurrent := make(map[int]bool)
	for s := h.current; ; {
		onCurrent[s] = true
		if s == 0 {
			break
		}
		c, ok := h.nodes[s]
		if !ok {
			return "", false
		}
		s = c.parent
	}
	// Walk the target's ancestry up to the first shared state, remembering the
	// downward path to replay.
	lca := seq
	var down []int
	for !onCurrent[lca] {
		c, ok := h.nodes[lca]
		if !ok {
			return "", false
		}
		down = append(down, lca)
		lca = c.parent
	}

	scratch := buffer.New(cur.Lines())
	for s := h.current; s != lca; {
		c := h.nodes[s]
		for i := len(c.Inverses) - 1; i >= 0; i-- {
			scratch.Apply(c.Inverses[i])
		}
		s = c.parent
	}
	for i := len(down) - 1; i >= 0; i-- {
		for _, e := range h.nodes[down[i]].Forwards {
			scratch.Apply(e)
		}
	}
	return scratch.String(), true
}

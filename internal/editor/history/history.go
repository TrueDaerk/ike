// Package history implements undo/redo over buffer edits. A Change bundles the
// forward edits of one user action with their inverses and the cursor position
// before and after, so undo restores both text and cursor. The store is an
// undo *tree* (#59): every state ever reached is a node keyed by its seq, a
// change pushed after an undo starts a sibling branch instead of discarding
// the redone-away future, and undo/redo walk the active branch so the default
// linear behavior is unchanged. Chronological navigation (vim's g-/g+) and
// jumping to an arbitrary node walk the tree along parent pointers.
package history

import (
	"sort"
	"strings"
	"time"

	"ike/internal/editor/buffer"
)

// Change is one undoable unit: the edits applied (in order) plus their inverses
// and the cursor positions bracketing the action.
type Change struct {
	Forwards     []buffer.Edit
	Inverses     []buffer.Edit
	CursorBefore buffer.Position
	CursorAfter  buffer.Position

	// At is the wall-clock time the change was committed (stamped by Push),
	// shown in the undo-tree view.
	At time.Time

	// seq numbers states globally (creation order, so g-/g+ order by it);
	// parent is the seq of the state this change was applied on top of.
	// Seq 0 is the root state (the loaded content) and has no Change.
	seq    int
	parent int
}

// maxNodes bounds the tree per buffer: past it, the oldest leaf branches are
// pruned (and, on a purely linear history, the oldest undo levels drop).
const maxNodes = 1000

// History is the undo store: a tree of Changes keyed by seq, with the current
// buffer state identified by the seq it sits at (0 = root).
type History struct {
	nodes    map[int]*Change // seq -> the change that created that state
	children map[int][]int   // parent seq -> child seqs, creation order
	active   map[int]int     // parent seq -> child redo follows (last visited)
	current  int             // seq of the state the buffer is at
	seq      int             // highest seq ever handed out

	// savedSeq is the seq of the current state when the buffer was last
	// written: 0 marks the initial (freshly loaded) state, -1 marks a
	// history none of whose states matches the file on disk (crash restore).
	// Undo/redo walking back to this exact state means the buffer is clean.
	savedSeq int
}

// New returns an empty history; its initial state counts as saved.
func New() *History { return &History{} }

// ensure lazily allocates the maps so the zero value keeps working.
func (h *History) ensure() {
	if h.nodes == nil {
		h.nodes = make(map[int]*Change)
		h.children = make(map[int][]int)
		h.active = make(map[int]int)
	}
}

// Reset drops the whole tree in place and re-baselines the empty state as
// saved (callers reset when installing content that matches disk). Callers
// that share the history across views (shared documents) use this instead of
// allocating a new History, so every alias sees the cleared store.
func (h *History) Reset() {
	h.nodes = nil
	h.children = nil
	h.active = nil
	h.current = 0
	h.savedSeq = 0
}

// CurrentSeq identifies the current state (0 = the root/loaded state).
func (h *History) CurrentSeq() int { return h.current }

// MarkSaved records the current state as the one written to disk.
func (h *History) MarkSaved() { h.savedSeq = h.current }

// MarkNeverSaved marks every state in this history as differing from disk —
// crash-restored content is dirty even after undoing back to it.
func (h *History) MarkNeverSaved() { h.savedSeq = -1 }

// AtSaved reports whether the current state is the last-saved one, so callers
// can clear their modified flag when undo/redo walks back to it. A saved state
// pruned out of the tree simply never reports true again until the next save.
func (h *History) AtSaved() bool { return h.savedSeq >= 0 && h.savedSeq == h.current }

// Push records a committed change as a new child of the current state. After
// an undo the abandoned future is kept: the new change becomes a sibling
// branch and redo follows it (it is now the active child).
func (h *History) Push(c Change) {
	h.ensure()
	h.seq++
	c.seq = h.seq
	c.parent = h.current
	if c.At.IsZero() {
		c.At = time.Now()
	}
	h.nodes[c.seq] = &c
	h.children[c.parent] = append(h.children[c.parent], c.seq)
	h.active[c.parent] = c.seq
	h.current = c.seq
	h.prune()
}

// CanUndo reports whether there is a change to undo.
func (h *History) CanUndo() bool { return h.current != 0 }

// CanRedo reports whether there is a change to redo.
func (h *History) CanRedo() bool { _, ok := h.redoChild(h.current); return ok }

// redoChild picks the child redo descends into from state seq: the active
// (last visited) child when recorded, else the newest branch.
func (h *History) redoChild(seq int) (int, bool) {
	if a, ok := h.active[seq]; ok {
		if _, live := h.nodes[a]; live {
			return a, true
		}
	}
	kids := h.children[seq]
	if len(kids) == 0 {
		return 0, false
	}
	return kids[len(kids)-1], true
}

// Undo reverts the most recent change against b and returns the cursor position
// to restore. ok is false when there is nothing to undo.
func (h *History) Undo(b *buffer.Buffer) (cursor buffer.Position, ok bool) {
	if h.current == 0 {
		return buffer.Position{}, false
	}
	h.ensure()
	c := h.nodes[h.current]
	for i := len(c.Inverses) - 1; i >= 0; i-- {
		b.Apply(c.Inverses[i])
	}
	h.active[c.parent] = c.seq // redo retraces this branch
	h.current = c.parent
	return c.CursorBefore, true
}

// Redo re-applies the change on the active branch against b and returns the
// cursor position to restore. ok is false when there is nothing to redo.
func (h *History) Redo(b *buffer.Buffer) (cursor buffer.Position, ok bool) {
	child, ok := h.redoChild(h.current)
	if !ok {
		return buffer.Position{}, false
	}
	c := h.nodes[child]
	for _, e := range c.Forwards {
		b.Apply(e)
	}
	h.current = child
	return c.CursorAfter, true
}

// UndoChrono moves to the previous state in global (seq) order regardless of
// branch — vim's g-. ok is false at the chronologically first state.
func (h *History) UndoChrono(b *buffer.Buffer) (cursor buffer.Position, ok bool) {
	target, found := -1, false
	for seq := range h.nodes {
		if seq < h.current && seq > target {
			target, found = seq, true
		}
	}
	if !found {
		if h.current == 0 {
			return buffer.Position{}, false
		}
		target = 0 // root is always reachable
	}
	return h.JumpTo(b, target)
}

// RedoChrono moves to the next state in global (seq) order — vim's g+.
func (h *History) RedoChrono(b *buffer.Buffer) (cursor buffer.Position, ok bool) {
	target, found := 0, false
	for seq := range h.nodes {
		if seq > h.current && (!found || seq < target) {
			target, found = seq, true
		}
	}
	if !found {
		return buffer.Position{}, false
	}
	return h.JumpTo(b, target)
}

// JumpTo restores the buffer to the state identified by seq, applying inverse
// edits up to the common ancestor and forward edits down to the target. The
// walked path becomes the active branch, so undo/redo continue from there.
// ok is false when seq is unknown or already current.
func (h *History) JumpTo(b *buffer.Buffer, seq int) (cursor buffer.Position, ok bool) {
	if seq == h.current {
		return buffer.Position{}, false
	}
	if seq != 0 {
		if _, live := h.nodes[seq]; !live {
			return buffer.Position{}, false
		}
	}
	h.ensure()

	// Ancestors of the current state (inclusive), for the LCA lookup.
	onCurrent := make(map[int]bool)
	for s := h.current; ; s = h.nodes[s].parent {
		onCurrent[s] = true
		if s == 0 {
			break
		}
	}
	// Walk the target's ancestry up to the first shared state; remember the
	// downward path (target..lca, exclusive) to replay forwards in order.
	lca := seq
	var down []int
	for !onCurrent[lca] {
		down = append(down, lca)
		lca = h.nodes[lca].parent
	}

	// Undo from current up to the LCA.
	var lastUndone *Change
	for s := h.current; s != lca; {
		c := h.nodes[s]
		for i := len(c.Inverses) - 1; i >= 0; i-- {
			b.Apply(c.Inverses[i])
		}
		h.active[c.parent] = c.seq
		lastUndone = c
		s = c.parent
	}
	// Redo from the LCA down to the target.
	for i := len(down) - 1; i >= 0; i-- {
		c := h.nodes[down[i]]
		for _, e := range c.Forwards {
			b.Apply(e)
		}
		h.active[c.parent] = c.seq
	}
	h.current = seq

	if len(down) > 0 {
		return h.nodes[seq].CursorAfter, true
	}
	return lastUndone.CursorBefore, true
}

// prune keeps the tree under maxNodes: oldest leaf branches go first; on a
// purely linear history the oldest undo level drops (its state becomes the
// new root, like vim's 'undolevels').
func (h *History) prune() {
	for len(h.nodes) > maxNodes {
		if h.removeOldestLeaf() {
			continue
		}
		if !h.dropOldestLevel() {
			return
		}
	}
}

// removeOldestLeaf deletes the lowest-seq leaf that is not the current state.
// It reports false when the tree is a single chain ending at current (the only
// leaf is current itself).
func (h *History) removeOldestLeaf() bool {
	victim := -1
	for seq := range h.nodes {
		if seq == h.current || len(h.children[seq]) > 0 {
			continue
		}
		if victim == -1 || seq < victim {
			victim = seq
		}
	}
	if victim == -1 {
		return false
	}
	h.removeLeaf(victim)
	return true
}

// removeLeaf unlinks a childless node from the tree.
func (h *History) removeLeaf(seq int) {
	parent := h.nodes[seq].parent
	kids := h.children[parent]
	for i, k := range kids {
		if k == seq {
			h.children[parent] = append(kids[:i], kids[i+1:]...)
			break
		}
	}
	if len(h.children[parent]) == 0 {
		delete(h.children, parent)
	}
	if h.active[parent] == seq {
		delete(h.active, parent)
	}
	delete(h.active, seq)
	delete(h.nodes, seq)
}

// dropOldestLevel removes the root's single child on a purely linear history:
// that change can no longer be undone and its state becomes the new root.
// Only valid when the root has exactly one child — with branches at the root,
// removeOldestLeaf always finds a victim first, so this never runs then.
func (h *History) dropOldestLevel() bool {
	kids := h.children[0]
	if len(kids) != 1 {
		return false
	}
	c := kids[0]
	if c == h.current {
		h.current = 0
	}
	// The dropped change's state is the new root: reparent its children to 0.
	grand := h.children[c]
	for _, g := range grand {
		h.nodes[g].parent = 0
	}
	h.children[0] = grand
	if len(grand) == 0 {
		delete(h.children, 0)
	}
	if a, ok := h.active[c]; ok {
		h.active[0] = a
	} else {
		delete(h.active, 0)
	}
	delete(h.active, c)
	delete(h.children, c)
	delete(h.nodes, c)
	switch h.savedSeq {
	case c:
		h.savedSeq = 0 // that state is the root now
	case 0:
		h.savedSeq = -1 // the old root state is gone
	}
	return true
}

// NodeInfo describes one state for the undo-tree view.
type NodeInfo struct {
	Seq     int
	Parent  int // -1 for the root
	At      time.Time
	Preview string // short excerpt of the change's first edit
	Edits   int
	Current bool
	Saved   bool
}

// Tree returns every state (root included) ordered by seq, for rendering the
// undo-tree view.
func (h *History) Tree() []NodeInfo {
	out := make([]NodeInfo, 0, len(h.nodes)+1)
	out = append(out, NodeInfo{
		Seq:     0,
		Parent:  -1,
		Current: h.current == 0,
		Saved:   h.savedSeq == 0,
	})
	for seq, c := range h.nodes {
		out = append(out, NodeInfo{
			Seq:     seq,
			Parent:  c.parent,
			At:      c.At,
			Preview: previewOf(*c),
			Edits:   len(c.Forwards),
			Current: h.current == seq,
			Saved:   h.savedSeq == seq,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// previewOf summarizes a change from its first edit: inserted text as +"...",
// a pure deletion as -"..." (the inverse carries the removed text).
func previewOf(c Change) string {
	if len(c.Forwards) == 0 {
		return ""
	}
	s := ""
	if t := c.Forwards[0].Text; t != "" {
		s = "+" + quoteExcerpt(t)
	} else if len(c.Inverses) > 0 {
		s = "-" + quoteExcerpt(c.Inverses[0].Text)
	}
	if len(c.Forwards) > 1 {
		s += " …"
	}
	return s
}

// quoteExcerpt flattens newlines and truncates to a short quoted excerpt.
func quoteExcerpt(s string) string {
	s = strings.ReplaceAll(s, "\n", "⏎")
	r := []rune(s)
	if len(r) > 24 {
		s = string(r[:24]) + "…"
	}
	return `"` + s + `"`
}

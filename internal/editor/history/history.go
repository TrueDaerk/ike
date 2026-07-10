// Package history implements undo/redo over buffer edits. A Change bundles the
// forward edits of one user action with their inverses and the cursor position
// before and after, so undo restores both text and cursor. The store is a linear
// past/future pair today, but Change carries parent/seq fields so a later undo
// *tree* can replace the walk without changing how the editor records changes.
package history

import "ike/internal/editor/buffer"

// Change is one undoable unit: the edits applied (in order) plus their inverses
// and the cursor positions bracketing the action.
type Change struct {
	Forwards     []buffer.Edit
	Inverses     []buffer.Edit
	CursorBefore buffer.Position
	CursorAfter  buffer.Position

	// seq and parent are unused by the linear walk but reserve the shape for a
	// future undo tree (each node points at the state it branched from).
	seq    int
	parent int
}

// History is the undo/redo store.
type History struct {
	past   []Change
	future []Change
	seq    int

	// savedSeq is the seq of the change on top of past when the buffer was
	// last written: 0 marks the initial (freshly loaded) state, -1 marks a
	// history none of whose states matches the file on disk (crash restore).
	// Undo/redo walking back to this exact state means the buffer is clean.
	savedSeq int
}

// New returns an empty history; its initial state counts as saved.
func New() *History { return &History{} }

// Reset drops all past and future changes in place and re-baselines the empty
// state as saved (callers reset when installing content that matches disk).
// Callers that share the history across views (shared documents) use this
// instead of allocating a new History, so every alias sees the cleared stack.
func (h *History) Reset() {
	h.past = h.past[:0]
	h.future = h.future[:0]
	h.savedSeq = 0
}

// topSeq identifies the current state: the seq of the change on top of past,
// or 0 for the initial state.
func (h *History) topSeq() int {
	if len(h.past) == 0 {
		return 0
	}
	return h.past[len(h.past)-1].seq
}

// MarkSaved records the current state as the one written to disk.
func (h *History) MarkSaved() { h.savedSeq = h.topSeq() }

// MarkNeverSaved marks every state in this history as differing from disk —
// crash-restored content is dirty even after undoing back to it.
func (h *History) MarkNeverSaved() { h.savedSeq = -1 }

// AtSaved reports whether the current state is the last-saved one, so callers
// can clear their modified flag when undo/redo walks back to it. A state
// abandoned by a post-undo edit (Push clears future) stays unreachable and
// AtSaved simply never reports true again until the next save.
func (h *History) AtSaved() bool { return h.savedSeq >= 0 && h.savedSeq == h.topSeq() }

// Push records a committed change and clears the redo stack (a new edit after an
// undo abandons the redone-away future, as in vim's linear undo).
func (h *History) Push(c Change) {
	h.seq++
	c.seq = h.seq
	if len(h.past) > 0 {
		c.parent = h.past[len(h.past)-1].seq
	}
	h.past = append(h.past, c)
	h.future = nil
}

// CanUndo reports whether there is a change to undo.
func (h *History) CanUndo() bool { return len(h.past) > 0 }

// CanRedo reports whether there is a change to redo.
func (h *History) CanRedo() bool { return len(h.future) > 0 }

// Undo reverts the most recent change against b and returns the cursor position
// to restore. ok is false when there is nothing to undo.
func (h *History) Undo(b *buffer.Buffer) (cursor buffer.Position, ok bool) {
	if len(h.past) == 0 {
		return buffer.Position{}, false
	}
	c := h.past[len(h.past)-1]
	h.past = h.past[:len(h.past)-1]
	for i := len(c.Inverses) - 1; i >= 0; i-- {
		b.Apply(c.Inverses[i])
	}
	h.future = append(h.future, c)
	return c.CursorBefore, true
}

// Redo re-applies the most recently undone change against b and returns the
// cursor position to restore. ok is false when there is nothing to redo.
func (h *History) Redo(b *buffer.Buffer) (cursor buffer.Position, ok bool) {
	if len(h.future) == 0 {
		return buffer.Position{}, false
	}
	c := h.future[len(h.future)-1]
	h.future = h.future[:len(h.future)-1]
	for _, e := range c.Forwards {
		b.Apply(e)
	}
	h.past = append(h.past, c)
	return c.CursorAfter, true
}

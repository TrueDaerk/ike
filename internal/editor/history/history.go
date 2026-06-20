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
}

// New returns an empty history.
func New() *History { return &History{} }

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

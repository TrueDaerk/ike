package history

import "ike/internal/editor/buffer"

// Recorder applies edits to a buffer while accumulating the forward edits and
// their inverses into a single Change. The editor opens a recorder at the start
// of an action, applies one or more edits, then commits the resulting Change to
// the History. This keeps "apply an edit" and "make it undoable" in one place so
// no caller can mutate the buffer without recording an inverse.
type Recorder struct {
	buf      *buffer.Buffer
	forwards []buffer.Edit
	inverses []buffer.Edit
	before   buffer.Position
}

// NewRecorder starts recording against buf, capturing cursorBefore for undo.
func NewRecorder(buf *buffer.Buffer, cursorBefore buffer.Position) *Recorder {
	return &Recorder{buf: buf, before: cursorBefore}
}

// Apply performs e and remembers it together with its inverse. It returns the
// end position of the inserted text so the caller can place the cursor.
func (r *Recorder) Apply(e buffer.Edit) buffer.Position {
	inv, end := r.buf.Apply(e)
	r.forwards = append(r.forwards, e)
	r.inverses = append(r.inverses, inv)
	return end
}

// Empty reports whether no edit was applied (so the caller can skip committing a
// no-op change).
func (r *Recorder) Empty() bool { return len(r.forwards) == 0 }

// Commit finalizes the recorded edits into a Change ending at cursorAfter.
func (r *Recorder) Commit(cursorAfter buffer.Position) Change {
	return Change{
		Forwards:     r.forwards,
		Inverses:     r.inverses,
		CursorBefore: r.before,
		CursorAfter:  cursorAfter,
	}
}

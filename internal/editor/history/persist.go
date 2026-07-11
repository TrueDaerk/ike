package history

import "ike/internal/editor/buffer"

// persist.go is the serialization seam for persistent undo (#148): Snapshot
// exports the past/future stacks (including the seq/parent fields reserved for
// the undo tree, #59) in a JSON-stable wire form, and RestoreSnapshot installs
// them into a History whose current buffer state is known to match the disk
// content the snapshot was taken against.

// Snapshot is the serializable form of a History's stacks. The zero value is
// an empty history.
type Snapshot struct {
	Past   []ChangeRecord `json:"past,omitempty"`
	Future []ChangeRecord `json:"future,omitempty"`
}

// Empty reports whether the snapshot carries no changes at all.
func (s Snapshot) Empty() bool { return len(s.Past) == 0 && len(s.Future) == 0 }

// ChangeRecord is the wire form of one Change, with the seq/parent fields
// exported so a serialized history keeps the shape a future undo tree needs.
type ChangeRecord struct {
	Forwards     []buffer.Edit   `json:"forwards,omitempty"`
	Inverses     []buffer.Edit   `json:"inverses,omitempty"`
	CursorBefore buffer.Position `json:"cursor_before"`
	CursorAfter  buffer.Position `json:"cursor_after"`
	Seq          int             `json:"seq"`
	Parent       int             `json:"parent,omitempty"`
}

// Snapshot exports the current stacks.
func (h *History) Snapshot() Snapshot {
	return Snapshot{Past: toRecords(h.past), Future: toRecords(h.future)}
}

// RestoreSnapshot replaces the stacks with s. The caller guarantees the
// current buffer content is the state the snapshot was taken at (persistent
// undo verifies this via a content hash before calling), so the restored top
// state counts as saved and the seq counter resumes past the highest restored
// seq — a change pushed after restore never collides with a restored one.
func (h *History) RestoreSnapshot(s Snapshot) {
	h.past = fromRecords(s.Past)
	h.future = fromRecords(s.Future)
	h.seq = 0
	for _, c := range append(s.Past, s.Future...) {
		if c.Seq > h.seq {
			h.seq = c.Seq
		}
	}
	h.savedSeq = h.topSeq()
}

func toRecords(cs []Change) []ChangeRecord {
	if len(cs) == 0 {
		return nil
	}
	out := make([]ChangeRecord, len(cs))
	for i, c := range cs {
		out[i] = ChangeRecord{
			Forwards:     c.Forwards,
			Inverses:     c.Inverses,
			CursorBefore: c.CursorBefore,
			CursorAfter:  c.CursorAfter,
			Seq:          c.seq,
			Parent:       c.parent,
		}
	}
	return out
}

func fromRecords(rs []ChangeRecord) []Change {
	if len(rs) == 0 {
		return nil
	}
	out := make([]Change, len(rs))
	for i, r := range rs {
		out[i] = Change{
			Forwards:     r.Forwards,
			Inverses:     r.Inverses,
			CursorBefore: r.CursorBefore,
			CursorAfter:  r.CursorAfter,
			seq:          r.Seq,
			parent:       r.Parent,
		}
	}
	return out
}

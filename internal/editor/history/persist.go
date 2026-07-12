package history

import (
	"sort"
	"time"

	"ike/internal/editor/buffer"
)

// persist.go is the serialization seam for persistent undo (#148): Snapshot
// exports the undo tree (#59) in a JSON-stable wire form, and RestoreSnapshot
// installs it into a History whose current buffer state is known to match the
// disk content the snapshot was taken against. Snapshots written before the
// tree (linear past/future stacks) still restore: the stacks are one chain,
// which is a degenerate tree.

// Snapshot is the serializable form of a History. The zero value is an empty
// history. Nodes+Current is the tree form; Past/Future are read-only legacy
// fields from pre-tree snapshots.
type Snapshot struct {
	Nodes   []ChangeRecord `json:"nodes,omitempty"`
	Current int            `json:"current,omitempty"`

	Past   []ChangeRecord `json:"past,omitempty"`
	Future []ChangeRecord `json:"future,omitempty"`
}

// Empty reports whether the snapshot carries no changes at all.
func (s Snapshot) Empty() bool {
	return len(s.Nodes) == 0 && len(s.Past) == 0 && len(s.Future) == 0
}

// ChangeRecord is the wire form of one Change.
type ChangeRecord struct {
	Forwards     []buffer.Edit   `json:"forwards,omitempty"`
	Inverses     []buffer.Edit   `json:"inverses,omitempty"`
	CursorBefore buffer.Position `json:"cursor_before"`
	CursorAfter  buffer.Position `json:"cursor_after"`
	At           time.Time       `json:"at,omitzero"`
	Seq          int             `json:"seq"`
	Parent       int             `json:"parent,omitempty"`
}

// Snapshot exports the tree, nodes ordered by seq.
func (h *History) Snapshot() Snapshot {
	records := make([]ChangeRecord, 0, len(h.nodes))
	for _, c := range h.nodes {
		records = append(records, toRecord(*c))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Seq < records[j].Seq })
	if len(records) == 0 {
		records = nil
	}
	return Snapshot{Nodes: records, Current: h.current}
}

// RestoreSnapshot replaces the store with s. The caller guarantees the
// current buffer content is the state the snapshot was taken at (persistent
// undo verifies this via a content hash before calling), so the restored
// current state counts as saved and the seq counter resumes past the highest
// restored seq — a change pushed after restore never collides with a restored
// one.
func (h *History) RestoreSnapshot(s Snapshot) {
	h.Reset()
	h.ensure()
	if len(s.Nodes) > 0 {
		for _, r := range s.Nodes {
			h.install(fromRecord(r))
		}
		h.current = s.Current
		if _, live := h.nodes[h.current]; !live && h.current != 0 {
			h.current = 0 // corrupt Current: fall back to the root
		}
		// Make redo retrace the path the snapshot was current at.
		for seq := h.current; seq != 0; {
			c := h.nodes[seq]
			h.active[c.parent] = seq
			seq = c.parent
		}
	} else {
		// Legacy linear form: past is the chain root -> current, future the
		// undone chain above it (top of the stack = next redo).
		for _, r := range s.Past {
			h.install(fromRecord(r))
			h.current = r.Seq
		}
		for _, r := range s.Future {
			h.install(fromRecord(r))
			// Redo must walk back down exactly this chain.
			h.active[r.Parent] = r.Seq
		}
	}
	h.seq = 0
	for seq := range h.nodes {
		if seq > h.seq {
			h.seq = seq
		}
	}
	h.savedSeq = h.current
}

// install links one restored change into the tree maps.
func (h *History) install(c Change) {
	cc := c
	h.nodes[cc.seq] = &cc
	h.children[cc.parent] = append(h.children[cc.parent], cc.seq)
}

func toRecord(c Change) ChangeRecord {
	return ChangeRecord{
		Forwards:     c.Forwards,
		Inverses:     c.Inverses,
		CursorBefore: c.CursorBefore,
		CursorAfter:  c.CursorAfter,
		At:           c.At,
		Seq:          c.seq,
		Parent:       c.parent,
	}
}

func fromRecord(r ChangeRecord) Change {
	return Change{
		Forwards:     r.Forwards,
		Inverses:     r.Inverses,
		CursorBefore: r.CursorBefore,
		CursorAfter:  r.CursorAfter,
		At:           r.At,
		seq:          r.Seq,
		parent:       r.Parent,
	}
}

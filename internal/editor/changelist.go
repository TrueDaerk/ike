package editor

// changelist.go implements vim's change list (#1174): a small per-document
// ring of recent edit positions that g; (older) and g, (newer) walk. Entries
// are the CursorAfter of every committed history.Change — the undo record
// already carries the exact post-edit position, so the ring is derived from
// the same push that makes the edit undoable (Model.pushChange) rather than
// from a parallel hook. The walk moves the CURSOR only: no undo (that is
// g-/g+), and no navigation-history entry — like the diagnostics motions,
// small in-file steps stay off the nav stack.
//
// Drift: entries shift with line-count changes through the same cheap delta
// scheme local marks use (shiftLocalMarks, called beside it from
// notifyMarkEdit) — exact for whole-line insertions/deletions, approximate
// for multi-line replacements — and every jump additionally clamps into the
// buffer, so residual drift can never land outside the text. The ring is
// per-view session state like the local marks: it resets with the undo
// history (Load/NewFile/RestoreText) and does not include changes restored
// from the persistent undo store.

import (
	"strconv"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
)

// changeListMax bounds the ring; the oldest entries drop first.
const changeListMax = 100

// changeList is the ring plus the g;/g, walk pointer. The zero value is an
// empty, not-walking list.
type changeList struct {
	pos []buffer.Position // oldest first, newest last
	// walk is the g;/g, position: 0 = not walking, n >= 1 = standing on the
	// n-th newest entry. Any new edit resets it, so the next g; starts from
	// the most recent edit again (vim semantics).
	walk int
}

// record appends the post-edit position p. Adjacent entries on the same line
// collapse into one that keeps the newest position, so a run of edits on one
// line reads as a single change-list stop. New edits reset the walk pointer.
func (c *changeList) record(p buffer.Position) {
	c.walk = 0
	if n := len(c.pos); n > 0 && c.pos[n-1].Line == p.Line {
		c.pos[n-1] = p
		return
	}
	c.pos = append(c.pos, p)
	if len(c.pos) > changeListMax {
		c.pos = c.pos[len(c.pos)-changeListMax:]
	}
}

// shift applies the local-mark delta scheme (shiftLocalMarks) to the ring
// after an edit changed the line count: insertions move entries at or below
// the edit row down, deletions pull the ones below the removed range up,
// clamped at the cursor row.
func (c *changeList) shift(cursorAfter, delta int) {
	if delta == 0 || len(c.pos) == 0 {
		return
	}
	threshold := cursorAfter - delta + 1
	if delta < 0 {
		threshold = cursorAfter + 1
	}
	for i, p := range c.pos {
		if p.Line < threshold {
			continue
		}
		p.Line += delta
		if p.Line < cursorAfter {
			p.Line = cursorAfter
		}
		if p.Line < 0 {
			p.Line = 0
		}
		c.pos[i] = p
	}
}

// pushChange commits a change to the undo history and flags its CursorAfter
// for the change list. The ring entry itself is recorded from emitChar's
// EventChange branch (recordPendingChange), which runs after the same-event
// mark/breakpoint/ring line-shift — recording here would let the edit's own
// delta shift the brand-new entry. Every hist.Push site goes through this
// wrapper.
func (m *Model) pushChange(c history.Change) {
	m.hist.Push(c)
	m.changePending = true
	m.changePos = c.CursorAfter
}

// recordPendingChange moves a flagged CursorAfter into the ring; called from
// emitChar on EventChange, after notifyMarkEdit shifted the existing entries.
// Change events without a pushed change (undo/redo, reload, encoding swaps)
// record nothing — vim's change list grows on changes, not on undo walks.
func (m *Model) recordPendingChange() {
	if !m.changePending {
		return
	}
	m.changePending = false
	m.changes.record(m.changePos)
}

// changeListJump implements g; (older=true) and g, (older=false): move the
// cursor count entries along the recorded edit positions. The first g;
// lands on the most recent edit position; the pointer survives until a new
// edit resets it. Cursor motion only — no undo, no nav-history entry.
func (m *Model) changeListJump(older bool, count int) {
	n := len(m.changes.pos)
	target := m.changes.walk
	switch {
	case older:
		target += count
		if target > n {
			target = n
		}
	case target > 0:
		target -= count
		if target < 1 {
			target = 1
		}
	}
	// (g, without a prior g; leaves target at 0: nothing is newer.)
	if n == 0 || target == m.changes.walk {
		if older {
			m.cmdMsg = "no earlier edit position"
		} else {
			m.cmdMsg = "no later edit position"
		}
		return
	}
	m.changes.walk = target
	// Clamp: later edits may have drifted the entry (see the file comment).
	m.moveTo(m.changes.pos[n-target])
	m.scroll()
	m.cmdMsg = "change list: " + strconv.Itoa(target) + "/" + strconv.Itoa(n)
}

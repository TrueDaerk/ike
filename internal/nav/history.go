// Package nav implements the editor navigation history (Roadmap 0220):
// cursor positions recorded per jump — file switches, go-to-definition,
// references picks — traversed with JetBrains Navigate Back/Forward
// semantics. The structure is pure data (no tea/app imports) so it
// unit-tests in isolation; the root model owns recording and navigation.
package nav

// Position is one remembered caret location. Line/Col are 0-based, matching
// the editor's internal convention (editor.SetCursor).
type Position struct {
	Path string
	Line int
	Col  int
}

// near reports whether two positions would read as "the same place" in a
// history: same file and same line (column drift on a line is not a jump).
func (p Position) near(o Position) bool {
	return p.Path == o.Path && p.Line == o.Line
}

// maxEntries bounds each direction's stack; the oldest back-entries fall off.
const maxEntries = 100

// History holds the back/forward stacks. The zero value is ready to use.
type History struct {
	back    []Position
	forward []Position
}

// RecordJump remembers from — the position the caret is leaving — as the
// newest back entry. A fresh jump invalidates the forward tail (standard
// editor semantics), consecutive near-identical positions collapse into one
// entry, and the stack is capped at maxEntries.
func (h *History) RecordJump(from Position) {
	if from.Path == "" {
		return // pathless editors (scratch tabs) hold no place worth returning to
	}
	h.forward = nil
	if n := len(h.back); n > 0 && h.back[n-1].near(from) {
		h.back[n-1] = from // keep the freshest column for the collapsed spot
		return
	}
	h.back = append(h.back, from)
	if len(h.back) > maxEntries {
		h.back = h.back[len(h.back)-maxEntries:]
	}
}

// Back steps to the previous position. current — where the caret is now — is
// pushed onto the forward stack so Forward can return. ok is false when
// there is nothing to go back to.
func (h *History) Back(current Position) (Position, bool) {
	return h.BackWhere(current, nil)
}

// BackWhere is Back with a validity filter (#220): entries valid rejects —
// deleted or renamed files — are dropped and traversal continues in the same
// direction. current lands on the forward stack only when a target is found.
// A nil valid accepts everything.
func (h *History) BackWhere(current Position, valid func(Position) bool) (Position, bool) {
	for len(h.back) > 0 {
		n := len(h.back) - 1
		target := h.back[n]
		h.back = h.back[:n]
		// A back target that equals the current spot is a stale self-entry
		// (e.g. the jump landed where it started): skip it, keep looking.
		if target.near(current) {
			continue
		}
		if valid != nil && !valid(target) {
			continue // stale entry: silently dropped (#220)
		}
		if current.Path != "" {
			h.forward = append(h.forward, current)
		}
		return target, true
	}
	return Position{}, false
}

// Forward re-traverses after a Back. current is pushed onto the back stack.
// ok is false when there is nothing ahead.
func (h *History) Forward(current Position) (Position, bool) {
	return h.ForwardWhere(current, nil)
}

// ForwardWhere is Forward with a validity filter; see BackWhere.
func (h *History) ForwardWhere(current Position, valid func(Position) bool) (Position, bool) {
	for len(h.forward) > 0 {
		n := len(h.forward) - 1
		target := h.forward[n]
		h.forward = h.forward[:n]
		if target.near(current) {
			continue
		}
		if valid != nil && !valid(target) {
			continue // stale entry: silently dropped (#220)
		}
		if current.Path != "" {
			h.back = append(h.back, current)
		}
		return target, true
	}
	return Position{}, false
}

// CanBack reports whether Back has anywhere to go.
func (h *History) CanBack() bool { return len(h.back) > 0 }

// CanForward reports whether Forward has anywhere to go.
func (h *History) CanForward() bool { return len(h.forward) > 0 }

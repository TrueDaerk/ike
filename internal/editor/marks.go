package editor

import (
	"sort"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/motion"
)

// marks.go implements vim marks and bookmarks (#1151). Local marks (m{a-z})
// are per-view positions like the cursor and the caret set — deliberately
// per-session state, cleared when the view loads another file. Global marks
// (m{A-Z}) live in the app-owned persistent store, reached through injected
// hooks exactly like the breakpoint store (breakpoints.go): the editor holds
// nothing, the gutter queries the source per frame, and cross-file jumps
// travel as a message the app resolves through its standard open funnel so
// the navigation history records them.
//
// Edit adjustment: marks shift with line-count changes through the same
// cheap delta scheme folds and breakpoints use (notifyMarkEdit) — exact for
// whole-line insertions/deletions above a mark, approximate for multi-line
// replacements. Jumps additionally clamp into the buffer, so residual drift
// can never land outside the text.

// GlobalMarkJumpMsg asks the root model to jump to global mark Letter,
// opening its file through the standard open flow when needed. Exact selects
// the backtick form (exact position) over the quote form (first non-blank of
// the line). Emitted by '{A-Z} / `{A-Z}.
type GlobalMarkJumpMsg struct {
	Letter rune
	Exact  bool
}

// LocalMark is one local mark for listings (the bookmarks picker).
type LocalMark struct {
	Letter    rune
	Line, Col int
}

// SetMarkHooks injects the global-mark store closures (#1151): set records a
// mark, lines reports the marked 0-based lines of a path for the gutter, and
// adjust shifts the store's marks after an edit changed the line count (the
// breakpoint adjuster's signature). Nil hooks disable global marks.
func (m *Model) SetMarkHooks(set func(r rune, path string, line, col int), lines func(path string) []int, adjust func(path string, cursorAfter, delta int)) {
	m.gmSet = set
	m.gmLines = lines
	m.gmAdjust = adjust
	m.markLines = m.buf.LineCount()
}

// localMarkName reports whether r names a local mark (a-z).
func localMarkName(r rune) bool { return r >= 'a' && r <= 'z' }

// globalMarkName reports whether r names a global mark (A-Z).
func globalMarkName(r rune) bool { return r >= 'A' && r <= 'Z' }

// setMark handles the char after `m`: a-z records a local mark at the
// cursor, A-Z records a global mark through the injected store.
func (m *Model) setMark(r rune) {
	switch {
	case localMarkName(r):
		if m.marks == nil {
			m.marks = map[rune]buffer.Position{}
		}
		m.marks[r] = m.cursor
		m.bumpRender() // the bookmark glyph appears in the gutter
	case globalMarkName(r) && m.gmSet != nil && m.HasFile():
		m.gmSet(r, m.path, m.cursor.Line, m.cursor.Col)
		m.bumpRender()
	}
}

// jumpMark handles the char after `'` or a backtick: local marks jump in
// place (clamped into the buffer — cheap-delta adjustment may drift on
// multi-line replacements); global marks resolve app-side, through the
// standard open funnel. Missing marks report on the ex line, vim's E20.
func (m *Model) jumpMark(r rune, exact bool) tea.Cmd {
	if globalMarkName(r) {
		if m.gmSet == nil {
			return nil
		}
		msg := GlobalMarkJumpMsg{Letter: r, Exact: exact}
		return func() tea.Msg { return msg }
	}
	pos, ok := m.marks[r]
	if !localMarkName(r) || !ok {
		m.cmdMsg = "E20: mark not set"
		return nil
	}
	pos = m.buf.ClampCursor(pos)
	if !exact {
		pos = motion.FirstNonBlank(m.buf, pos, 1).Pos
	}
	m.jumpTo(pos) // records the departure in the nav history (EventJump)
	return nil
}

// JumpToLocalMark jumps to local mark r's exact position (the bookmarks
// picker's activation); false when the mark is unset.
func (m *Model) JumpToLocalMark(r rune) bool {
	pos, ok := m.marks[r]
	if !ok {
		return false
	}
	m.jumpTo(m.buf.ClampCursor(pos))
	m.scroll()
	return true
}

// RemoveLocalMark drops local mark r (the picker's aux action).
func (m *Model) RemoveLocalMark(r rune) {
	if _, ok := m.marks[r]; !ok {
		return
	}
	delete(m.marks, r)
	m.bumpRender()
}

// LocalMarks lists this view's local marks sorted by letter, positions
// clamped into the buffer (the same lazy clamp a jump applies).
func (m Model) LocalMarks() []LocalMark {
	out := make([]LocalMark, 0, len(m.marks))
	for r, pos := range m.marks {
		pos = m.buf.ClampCursor(pos)
		out = append(out, LocalMark{Letter: r, Line: pos.Line, Col: pos.Col})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Letter < out[j].Letter })
	return out
}

// MoveToFirstNonBlank places the cursor on the current line's first
// non-blank column — the app-side tail of a '{A-Z} jump after openPathAt
// landed on the mark's line.
func (m *Model) MoveToFirstNonBlank() {
	pos := motion.FirstNonBlank(m.buf, m.cursor, 1).Pos
	m.SetCursor(pos.Line, pos.Col)
}

// LineText returns the content of 0-based line i ("" out of range) — the
// bookmarks picker's preview source.
func (m Model) LineText(i int) string {
	if i < 0 || i >= m.buf.LineCount() {
		return ""
	}
	return m.buf.Line(i)
}

// bookmarkSet snapshots the marked lines for the gutter: this view's local
// marks plus the global marks recorded for the open file.
func (m Model) bookmarkSet() map[int]bool {
	if len(m.marks) == 0 && (m.gmLines == nil || !m.HasFile()) {
		return nil
	}
	set := map[int]bool{}
	for _, pos := range m.marks {
		set[m.buf.ClampCursor(pos).Line] = true
	}
	if m.gmLines != nil && m.HasFile() {
		for _, l := range m.gmLines(m.path) {
			if l >= 0 && l < m.buf.LineCount() {
				set[l] = true
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// notifyMarkEdit runs on every EventChange, beside notifyBreakpointEdit: it
// tracks the buffer's line count and shifts marks by the delta at the edit
// site — local marks in place, global marks through the injected adjuster.
// Wholesale buffer replacements (Load, share, restore) re-baseline via
// seedMarkLines instead of reporting.
func (m *Model) notifyMarkEdit() {
	lc := m.buf.LineCount()
	delta := lc - m.markLines
	m.markLines = lc
	if delta == 0 {
		return
	}
	m.shiftLocalMarks(m.cursor.Line, delta)
	if m.gmAdjust != nil && m.HasFile() {
		m.gmAdjust(m.path, m.cursor.Line, delta)
	}
}

// shiftLocalMarks applies the breakpoint store's shift semantics
// (debug.Breakpoints.AdjustEdit) to the local marks: insertions move marks
// at or below the insertion point down, deletions pull the ones below the
// removed range up, clamped at the cursor row.
func (m *Model) shiftLocalMarks(cursorAfter, delta int) {
	if len(m.marks) == 0 {
		return
	}
	threshold := cursorAfter - delta + 1
	if delta < 0 {
		threshold = cursorAfter + 1
	}
	for r, pos := range m.marks {
		if pos.Line < threshold {
			continue
		}
		pos.Line += delta
		if pos.Line < cursorAfter {
			pos.Line = cursorAfter
		}
		if pos.Line < 0 {
			pos.Line = 0
		}
		m.marks[r] = pos
	}
}

// seedMarkLines re-baselines the line counter after a wholesale buffer
// replacement, so the swap never reads as an edit; called wherever
// seedBreakpointLines is. Load paths also clear the local marks — they
// belong to the previous content.
func (m *Model) seedMarkLines() { m.markLines = m.buf.LineCount() }

// clearLocalMarks drops every local mark (a new file identity).
func (m *Model) clearLocalMarks() { m.marks = nil }

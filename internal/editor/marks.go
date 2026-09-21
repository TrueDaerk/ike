package editor

import (
	"sort"
	"unicode"

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

// MarkHooks bundles the global-mark store closures (#1151): Set records a
// mark, At reports whether a mark already sits on a line (the m{A-Z} toggle,
// #2661), Remove drops one, Letters reports a path's marked 0-based lines
// with their letter for the gutter, and Adjust shifts the store's marks
// after an edit changed the line count (the breakpoint adjuster's
// signature). A zero value disables global marks.
type MarkHooks struct {
	Set     func(r rune, path string, line, col int)
	At      func(r rune, path string, line int) bool
	Remove  func(r rune)
	Letters func(path string) map[int]rune
	Adjust  func(path string, cursorAfter, delta int)
}

// SetMarkHooks injects the global-mark store closures; the zero value
// disables global marks.
func (m *Model) SetMarkHooks(h MarkHooks) {
	m.gm = h
	m.markLines = m.buf.LineCount()
}

// SetBookmarkHooks injects the project bookmark store's closures (#55):
// signs reports a file's gutter glyphs by 0-based line (a mnemonic digit or
// the anonymous flag), adjust shifts the store's bookmarks after an edit
// changed the line count. Nil hooks disable project bookmarks.
func (m *Model) SetBookmarkHooks(signs func(path string) map[int]string, adjust func(path string, cursorAfter, delta int)) {
	m.bmSigns = signs
	m.bmAdjust = adjust
}

// localMarkName reports whether r names a local mark (a-z).
func localMarkName(r rune) bool { return r >= 'a' && r <= 'z' }

// globalMarkName reports whether r names a global mark (A-Z).
func globalMarkName(r rune) bool { return r >= 'A' && r <= 'Z' }

// setMark handles the char after `m`: a-z records a local mark at the
// cursor, A-Z records a global mark through the injected store. Repeating
// the key on the mark's own line removes it instead (#2661) — the toggle
// compares by line, not by column, because the user thinks in marked lines
// and the cursor column rarely matches the recorded one. A mark sitting on
// another line still moves to the cursor. The removal reports on the ex
// line, so the toggle is visible with the gutter hidden.
func (m *Model) setMark(r rune) {
	switch {
	case localMarkName(r):
		if pos, ok := m.marks[r]; ok && m.buf.ClampCursor(pos).Line == m.cursor.Line {
			delete(m.marks, r)
			m.cmdMsg = "mark " + string(r) + " removed"
			m.bumpRender()
			return
		}
		if m.marks == nil {
			m.marks = map[rune]buffer.Position{}
		}
		m.marks[r] = m.cursor
		m.bumpRender() // the mark's letter appears in the gutter
	case globalMarkName(r) && m.gm.Set != nil && m.HasFile():
		if m.gm.At != nil && m.gm.Remove != nil && m.gm.At(r, m.path, m.cursor.Line) {
			m.gm.Remove(r)
			m.cmdMsg = "mark " + string(r) + " removed"
			m.bumpRender()
			return
		}
		m.gm.Set(r, m.path, m.cursor.Line, m.cursor.Col)
		m.bumpRender()
	}
}

// jumpMark handles the char after `'` or a backtick: local marks jump in
// place (clamped into the buffer — cheap-delta adjustment may drift on
// multi-line replacements); global marks resolve app-side, through the
// standard open funnel. Missing marks report on the ex line, vim's E20.
func (m *Model) jumpMark(r rune, exact bool) tea.Cmd {
	if globalMarkName(r) {
		if m.gm.Set == nil {
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

// markLetterBefore reports whether vim mark a outranks b for the single
// gutter cell (#2661): alphabetically first wins, lowercase before uppercase
// for the same letter (the local mark is this view's own).
func markLetterBefore(a, b rune) bool {
	la, lb := unicode.ToLower(a), unicode.ToLower(b)
	if la != lb {
		return la < lb
	}
	return unicode.IsLower(a)
}

// vimMarkLetters collects the gutter letter of every vim mark on the open
// document: this view's local marks (m{a-z}) plus the global marks recorded
// for the file (m{A-Z}). One cell per line, so a line carrying several marks
// keeps the markLetterBefore winner.
func (m Model) vimMarkLetters() map[int]rune {
	out := map[int]rune{}
	add := func(l int, r rune) {
		if l < 0 || l >= m.buf.LineCount() {
			return
		}
		if cur, ok := out[l]; !ok || markLetterBefore(r, cur) {
			out[l] = r
		}
	}
	for r, pos := range m.marks {
		add(m.buf.ClampCursor(pos).Line, r)
	}
	if m.HasFile() && m.gm.Letters != nil {
		for l, r := range m.gm.Letters(m.path) {
			add(l, r)
		}
	}
	return out
}

// bookmarkSigns snapshots the gutter glyphs for the marked lines: a vim mark
// draws its own letter (#2661 — with several marks in a file the picker was
// the only way to tell them apart), a project bookmark (#55) its mnemonic
// digit or the anonymous flag. Precedence in the single sign cell: mnemonic
// digit > vim mark letter > anonymous "⚑" — the digit and the letter carry
// information the flag does not.
func (m Model) bookmarkSigns() map[int]string {
	set := map[int]string{}
	for l, r := range m.vimMarkLetters() {
		set[l] = string(r)
	}
	if m.HasFile() && m.bmSigns != nil {
		for l, sign := range m.bmSigns(m.path) {
			if l < 0 || l >= m.buf.LineCount() {
				continue
			}
			if _, marked := set[l]; marked && sign == "⚑" {
				continue // a vim mark letter outranks the anonymous flag
			}
			set[l] = sign
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
	// The change-list ring drifts with the same scheme (#1174).
	m.changes.shift(m.cursor.Line, delta)
	if m.HasFile() {
		if m.gm.Adjust != nil {
			m.gm.Adjust(m.path, m.cursor.Line, delta)
		}
		if m.bmAdjust != nil {
			m.bmAdjust(m.path, m.cursor.Line, delta)
		}
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

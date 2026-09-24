package editor

// matchscroll.go — match-aware horizontal scrolling for search landings
// (#2732). The ordinary follow logic only keeps the caret's column inside the
// window, so a search landing right of it parked the match's first character
// in the pane's last column with the rest of the match off-screen — easy to
// read as "nothing found". A landing instead reveals the whole match plus
// matchScrollMargin columns of context; a match wider than the window starts
// at its left edge. Ordinary cursor motion never sets a landing and keeps the
// plain follow behaviour.

import "ike/internal/editor/search"

// matchScrollMargin is the context kept right of a revealed search match.
const matchScrollMargin = 5

// landOnMatch records the match of q the cursor now sits on as the pending
// landing the next scroll() reveals. A cursor on no match (should not happen
// for a search landing) leaves nothing pending.
func (m *Model) landOnMatch(q search.Query) {
	m.landMatchOK = false
	// The match starts at the cursor: on a very long line only the text
	// from there on is scanned (#2734).
	for _, sp := range q.LineMatchesIn(m.buf, m.cursor.Line, m.cursor.Col, m.cursor.Col+search.LongLineBytes) {
		if m.cursor.Col >= sp.Start && m.cursor.Col < sp.End {
			m.landMatch = sp
			m.landMatchOK = true
			return
		}
	}
}

// matchScrollFix widens scroll()'s caret follow to the pending landing's whole
// match. It runs after the cursor-column follow and the conceal fix-up, so
// the match start is already inside the window; it only scrolls further right
// when the match end is not. Columns are compared in display cells — sv
// tables through svDisplayCol, conceal stand-ins and wide glyphs through the
// display prefix — exactly as the caret column is.
func (m *Model) matchScrollFix() {
	if !m.landMatchOK || m.softWrap {
		return
	}
	sp := m.landMatch
	line := m.cursor.Line
	if sp.Line != line || m.cursor.Col < sp.Start || m.cursor.Col >= sp.End {
		return
	}
	tw := m.scrollTextWidth()
	lineLen := len([]rune(m.buf.Line(line)))
	end := min(sp.End, lineLen)

	if m.svActive() {
		// view.Left is a display offset in table mode (#1724).
		s, e := m.svDisplayCol(line, sp.Start), m.svDisplayCol(line, end)
		m.view.Left = matchLeft(m.view.Left, s, e, m.svDisplayCol(line, lineLen), tw)
		return
	}
	prefix := m.displayPrefix(line)
	if prefix == nil {
		m.view.Left = matchLeft(m.view.Left, sp.Start, end, lineLen, tw)
		return
	}
	// Conceal / wide-glyph lines keep view.Left a buffer column (#1752):
	// derive the display target, then walk the offset right until it holds.
	disp := func(c int) int { return concealDisplayColAt(prefix, c) }
	target := matchLeft(disp(m.view.Left), disp(sp.Start), disp(end), disp(lineLen), tw)
	for m.view.Left < sp.Start && disp(m.view.Left) < target {
		m.view.Left++
	}
	if cr, ok := rangeAt(m.lineConcealRanges(line), m.view.Left); ok && m.view.Left > cr.start {
		m.view.Left = cr.end
	}
}

// matchLeft is the display offset revealing the match [s, e) in a window of
// tw cells starting at left: unchanged when the match is fully visible,
// otherwise the match end plus matchScrollMargin (cut at the line end
// lineEnd) becomes the rightmost visible cell, or — for a match that does not
// fit — its start becomes the leftmost.
func matchLeft(left, s, e, lineEnd, tw int) int {
	if tw < 1 {
		tw = 1
	}
	if s >= left && e <= left+tw {
		return left
	}
	right := e + matchScrollMargin
	if limit := max(lineEnd, e); right > limit {
		right = limit
	}
	if right-s > tw {
		return s
	}
	return max(right-tw, 0)
}

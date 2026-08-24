// Package textsel is the shared mouse text-selection engine for read-only
// panes (#2070): the click-streak gestures the terminal (#227, #951) and the
// HTTP response viewer (#1266) established — a drag selects by character, a
// double click selects a word and a triple click a line, with drags extending
// by the unit the streak chose. The engine works in an abstract grid of
// (line, column) positions; the owning view maps mouse cells onto positions,
// provides each line's display runes, and extracts the selected text itself.
package textsel

import "time"

// Pos is a caret position in the owning view's composed grid: a line index
// plus a display column into that line.
type Pos struct {
	Line, Col int
}

// Before reports whether p sorts ahead of q in reading order.
func (p Pos) Before(q Pos) bool {
	if p.Line != q.Line {
		return p.Line < q.Line
	}
	return p.Col < q.Col
}

// Mode is the unit a drag extends by, set by the click streak.
type Mode int

const (
	Char Mode = iota
	Word
	Line
)

// MultiClickWindow is how quickly a follow-up press on the same cell must
// arrive to grow the click streak — the terminal pane's value (#951).
const MultiClickWindow = 500 * time.Millisecond

// LineText provides the display runes of one grid line, nil past the end.
type LineText func(line int) []rune

// Selection is the mouse-selection state of one view.
type Selection struct {
	on           bool
	anchor, head Pos
	mode         Mode
	// originA/originB delimit the unit the streak started on, so extending a
	// word/line selection never eats into it.
	originA, originB Pos

	dragging    bool
	clickStreak int
	lastAt      time.Time
	lastPos     Pos
}

// Press anchors a selection at p. The click streak cycles char → word → line.
func (s *Selection) Press(p Pos, text LineText) {
	now := Now()
	if p == s.lastPos && now.Sub(s.lastAt) <= MultiClickWindow {
		s.clickStreak++
	} else {
		s.clickStreak = 1
	}
	s.lastAt, s.lastPos = now, p
	s.dragging = true
	s.on = false

	switch (s.clickStreak-1)%3 + 1 {
	case 2:
		s.mode = Word
		a, b, ok := wordSpanAt(p, text)
		if !ok {
			s.mode = Char
			s.anchor, s.head = p, p
			return
		}
		s.originA, s.originB = a, b
		s.anchor, s.head, s.on = a, b, true
	case 3:
		s.mode = Line
		a, b := lineSpanAt(p.Line, text)
		s.originA, s.originB = a, b
		s.anchor, s.head, s.on = a, b, true
	default:
		s.mode = Char
		s.anchor, s.head = p, p
	}
}

// Drag extends the selection to p in the unit the press chose.
func (s *Selection) Drag(p Pos, text LineText) {
	if !s.dragging {
		return
	}
	switch s.mode {
	case Word:
		a, b, ok := wordSpanAt(p, text)
		if !ok {
			a, b = p, Pos{Line: p.Line, Col: p.Col + 1}
		}
		s.extendUnit(a, b)
	case Line:
		a, b := lineSpanAt(p.Line, text)
		s.extendUnit(a, b)
	default:
		s.head = p
		s.on = s.head != s.anchor
	}
}

// Release ends the drag; the selection stays visible until it is copied,
// cleared, or a new press lands.
func (s *Selection) Release() { s.dragging = false }

// extendUnit grows a word/line selection to cover [a, b) without ever
// shrinking below the unit the streak started on (#951).
func (s *Selection) extendUnit(a, b Pos) {
	switch {
	case a.Before(s.originA):
		s.anchor, s.head = s.originB, a
	case s.originB.Before(b):
		s.anchor, s.head = s.originA, b
	default:
		s.anchor, s.head = s.originA, s.originB
	}
	s.on = s.anchor != s.head
}

// Active reports whether a selection exists.
func (s *Selection) Active() bool { return s.on }

// Clear drops the selection and any drag in progress.
func (s *Selection) Clear() { *s = Selection{} }

// Range returns the normalized selection span: the earlier endpoint
// (inclusive) first, the later (exclusive) second; ok is false without a
// selection.
func (s *Selection) Range() (start, end Pos, ok bool) {
	if !s.on {
		return start, end, false
	}
	start, end = s.anchor, s.head
	if end.Before(start) {
		start, end = end, start
	}
	return start, end, true
}

// LineRange returns the display-column interval [a, b) the selection covers
// on one grid line, given that line's rune count; a >= b means none.
func (s *Selection) LineRange(line, textLen int) (a, b int) {
	start, end, ok := s.Range()
	if !ok || line < start.Line || line > end.Line {
		return 0, 0
	}
	a, b = 0, textLen
	if line == start.Line {
		a = start.Col
	}
	if line == end.Line {
		b = min(end.Col, textLen)
	}
	return a, b
}

// IsWordRune reports whether r belongs to a word for double-click selection.
// The set is deliberately wide: code and response text are full of ids,
// tokens and URLs that a user double-clicks as one thing.
func IsWordRune(r rune) bool {
	if r == ' ' || r == '\t' {
		return false
	}
	switch r {
	case '"', '\'', ',', '{', '}', '[', ']', '(', ')', '<', '>', ';':
		return false
	}
	return true
}

// wordSpanAt returns the [a, b) span of the word under p. A non-word cell
// spans just itself; ok=false on blanks and past the end of a line.
func wordSpanAt(p Pos, text LineText) (a, b Pos, ok bool) {
	runes := text(p.Line)
	if p.Col >= len(runes) {
		return a, b, false
	}
	if !IsWordRune(runes[p.Col]) {
		return Pos{p.Line, p.Col}, Pos{p.Line, p.Col + 1}, true
	}
	start, end := p.Col, p.Col+1
	for start > 0 && IsWordRune(runes[start-1]) {
		start--
	}
	for end < len(runes) && IsWordRune(runes[end]) {
		end++
	}
	return Pos{p.Line, start}, Pos{p.Line, end}, true
}

// lineSpanAt returns the span covering the whole line.
func lineSpanAt(line int, text LineText) (a, b Pos) {
	return Pos{line, 0}, Pos{line, len(text(line))}
}

// RawSlice returns the substring of the raw line whose tab-expanded display
// columns intersect [a, b): selection works over the rendered cells, but the
// clipboard gets the real text — a partially covered tab is included whole.
func RawSlice(raw string, a, b, tabWidth int) string {
	if a >= b {
		return ""
	}
	runes := []rune(raw)
	col := 0
	from, to := len(runes), len(runes)
	for i, r := range runes {
		w := 1
		if r == '\t' {
			w = tabWidth
		}
		if col < b && col+w > a {
			if i < from {
				from = i
			}
			to = i + 1
		}
		col += w
	}
	if from >= to {
		return ""
	}
	return string(runes[from:to])
}

// Now is a seam so click-streak tests are not wall-clock dependent.
var Now = time.Now

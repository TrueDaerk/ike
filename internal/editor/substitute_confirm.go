package editor

import (
	"fmt"
	"regexp"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
)

// subConfirmState drives the interactive ":s///c" confirmation. It walks the
// precomputed hits in reading order, applying accepted replacements into one
// open recorder (a single undo unit).
//
// Accepted replacements move the text under the hits that follow, so each hit's
// original range is mapped through two running shifts (#2600): lineShift is the
// net number of lines the replacements so far added or removed — a replacement
// carrying a line break, or a match that spanned lines, changes it — and
// colDelta is the rune-column shift on the line a replacement ended on, which
// is where the next hit on that same original line now lives.
// curLine/curStart/curEnd is the span the view highlights; for a match
// spanning lines that is its part on the first line, which is where the cursor
// goes too.
type subConfirmState struct {
	repl                      string
	hits                      []subMatch
	idx                       int
	lineShift                 int
	colDelta                  map[int]int
	rec                       *history.Recorder
	replaced                  int
	touched                   map[int]bool
	curLine, curStart, curEnd int
}

// beginSubstituteConfirm collects the matches over [start,end] and enters the
// confirmation sub-state on the first one. Without the "g" flag only the first
// match per line is offered, matching vim. spanning selects the joined-range
// scan, the one that can see a match crossing a line boundary (#2600).
func (m Model) beginSubstituteConfirm(re *regexp.Regexp, repl string, global, spanning bool, reach, start, end int, pat string) Model {
	hits := collectSubMatches(m.buf, re, global, spanning, reach, start, end)
	if len(hits) == 0 {
		m.cmdMsg = "E: pattern not found: " + pat
		return m
	}
	m.subConfirm = &subConfirmState{
		repl:     repl,
		hits:     hits,
		colDelta: map[int]int{},
		rec:      history.NewRecorder(m.buf, m.cursor),
		touched:  map[int]bool{},
	}
	m.mode = Command // capture single-letter keys; the prompt renders on the ":" row
	m = m.focusSubMatch()
	m.cmdMsg = "replace (y/n/a/q/l)?"
	return m
}

// updateSubConfirm handles one keypress while the confirmation prompt is open.
func (m Model) updateSubConfirm(key tea.KeyPressMsg) Model {
	if key.Code == tea.KeyEscape {
		return m.finishSubConfirm()
	}
	r, ok := firstRune(key)
	if !ok {
		return m
	}
	switch r {
	case 'y':
		m.applyCurrentMatch()
		return m.advanceSubConfirm()
	case 'n':
		return m.advanceSubConfirm()
	case 'l': // replace this one, then stop
		m.applyCurrentMatch()
		return m.finishSubConfirm()
	case 'q': // stop without replacing this one
		return m.finishSubConfirm()
	case 'a': // replace this and every remaining match
		sc := m.subConfirm
		for sc.idx < len(sc.hits) {
			m.applyCurrentMatch()
			sc.idx++
		}
		return m.finishSubConfirm()
	}
	return m // any other key waits for a valid answer
}

// mapPos maps an original buffer position through the shifts the already
// applied replacements introduced.
func (sc *subConfirmState) mapPos(p buffer.Position) buffer.Position {
	return buffer.Position{Line: p.Line + sc.lineShift, Col: p.Col + sc.colDelta[p.Line]}
}

// focusSubMatch positions the cursor on the current hit and records its span for
// the view to highlight, mapping the original span through the running shifts.
// A hit spanning lines highlights its part of the first line, out to the line
// end — the rest of the match is on the lines below, which the prompt is about
// to rewrite anyway.
func (m Model) focusSubMatch() Model {
	sc := m.subConfirm
	h := sc.hits[sc.idx]
	s, e := sc.mapPos(h.start), sc.mapPos(h.end)
	sc.curLine, sc.curStart = s.Line, s.Col
	if e.Line == s.Line {
		sc.curEnd = e.Col
	} else {
		sc.curEnd = m.buf.RuneLen(s.Line)
	}
	m.cursor = m.buf.ClampCursor(s)
	m.desiredCol = m.cursor.Col
	return m
}

// applyCurrentMatch replaces the current hit through the open recorder and
// grows the shifts by what the replacement did, so later hits stay aligned: the
// line count it changed feeds lineShift, and where it left the end of the
// original last line feeds that line's column delta.
func (m *Model) applyCurrentMatch() {
	sc := m.subConfirm
	h := sc.hits[sc.idx]
	s, e := sc.mapPos(h.start), sc.mapPos(h.end)
	text := expandRepl(sc.repl, h.groups)
	end := sc.rec.Apply(buffer.Edit{
		Range: buffer.Range{Start: s, End: e},
		Text:  text,
	})
	// The text that followed the match on its last line now continues at end,
	// so a later hit on that original line shifts by the difference. lineShift
	// takes the net line-count change; end already accounts for the old one.
	sc.lineShift += (end.Line - s.Line) - (e.Line - s.Line)
	sc.colDelta[h.end.Line] = end.Col - h.end.Col
	sc.replaced++
	for l := h.start.Line; l <= h.end.Line; l++ {
		sc.touched[l] = true
	}
}

// advanceSubConfirm moves to the next hit, or finishes when none remain.
func (m Model) advanceSubConfirm() Model {
	sc := m.subConfirm
	sc.idx++
	if sc.idx >= len(sc.hits) {
		return m.finishSubConfirm()
	}
	return m.focusSubMatch()
}

// finishSubConfirm commits the accumulated replacements as one undo unit, leaves
// the sub-state, and reports the outcome. Already-applied replacements are kept.
func (m Model) finishSubConfirm() Model {
	sc := m.subConfirm
	m.subConfirm = nil
	m.mode = Normal

	lastLine := m.cursor.Line
	if sc.replaced > 0 {
		hi := -1
		for l := range sc.touched {
			if l > hi {
				hi = l
			}
		}
		if hi >= 0 {
			// touched holds *original* line numbers; the replacements may have
			// moved them (#2600), so the running shift maps the last one back.
			lastLine = hi + sc.lineShift
		}
	}
	cursorAfter := m.buf.ClampCursor(buffer.Position{Line: lastLine, Col: 0})
	if !sc.rec.Empty() {
		m.pushChange(sc.rec.Commit(cursorAfter))
		m.dirty = true
		m.emit(EventChange)
		m.cursor = cursorAfter
		m.desiredCol = cursorAfter.Col
	}
	if sc.replaced > 0 {
		m.cmdMsg = fmt.Sprintf("%d substitution%s on %d line%s", sc.replaced, plural(sc.replaced, "s"), len(sc.touched), plural(len(sc.touched), "s"))
	} else {
		m.cmdMsg = ""
	}
	return m
}

// byteToRune converts a byte offset within s to a rune column.
func byteToRune(s string, byteOff int) int {
	n := 0
	for i := range s {
		if i >= byteOff {
			break
		}
		n++
	}
	return n
}

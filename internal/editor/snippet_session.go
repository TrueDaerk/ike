package editor

import (
	"ike/internal/editor/buffer"
	"ike/internal/lsp/snippet"
)

// snippet_session.go is the tabstop session behind an accepted snippet
// completion (#846): after internal/lsp/snippet expands the insert text, the
// editor jumps the cursor between the expansion's tabstops with tab/shift+tab.
// Stops are absolute rune spans into the buffer; a placeholder's default text
// is pre-selected on jump (#2146) — the span highlights like a selection, the
// first typed rune replaces it, tabbing on keeps it. Typing between jumps is
// assumed to happen at the current stop, so on each jump the buffer-size delta
// since the last one shifts every later stop (the sequential fill-in shape).
// Esc (leaving insert mode) ends the session.
type snippetSession struct {
	stops    []snippetStop // absolute buffer rune spans, in visit order
	idx      int
	baseSize int // buffer rune size at the last rebase
	// selFrom/selTo mark the current stop's pre-selected placeholder text
	// (#2146), valid while selActive. The selection lives exactly from the
	// jump to the next cursor-moving key: the replace-on-type hook consumes
	// it, and any other cursor departure from selTo invalidates it (see
	// snippetSelActive), so no other edit path needs to clear it.
	selFrom, selTo buffer.Position
	selActive      bool
}

// snippetStop is one tabstop's absolute rune span: start..end covers the
// placeholder default (start == end for a bare stop). The cursor jumps to
// end, matching the pre-#2146 offsets.
type snippetStop struct {
	start, end int
}

// startSnippetSession installs the session for text just inserted ending at
// the cursor: rel are tabstop spans relative to the insert start. A single
// bare trailing stop is pointless (the cursor already sits there), so it
// starts no session.
func (m *Model) startSnippetSession(text string, rel []snippet.Stop) {
	n := len([]rune(text))
	startOff := m.posToOffset(m.cursor) - n
	if len(rel) == 1 && rel[0].Start == n && rel[0].End == n {
		return
	}
	s := &snippetSession{stops: make([]snippetStop, len(rel)), baseSize: m.bufSize()}
	for i, r := range rel {
		s.stops[i] = snippetStop{start: startOff + r.Start, end: startOff + r.End}
	}
	m.snippet = s
	m.snippetJumpTo(0)
}

// snippetJumpTo moves the cursor to stop i's end and pre-selects the
// placeholder span when it is non-empty (#2146).
func (m *Model) snippetJumpTo(i int) {
	s := m.snippet
	st := s.stops[i]
	m.cursor = m.buf.Clamp(m.offsetToPos(st.end))
	m.desiredCol = m.cursor.Col
	s.selActive = false
	if st.start < st.end {
		s.selFrom = m.buf.Clamp(m.offsetToPos(st.start))
		s.selTo = m.cursor
		s.selActive = true
	}
	m.emit(EventCursorMove)
}

// snippetMove jumps delta stops (+1 tab, -1 shift+tab). Moving past the last
// stop ends the session with the cursor on it; before the first clamps.
func (m *Model) snippetMove(delta int) {
	s := m.snippet
	if s == nil {
		return
	}
	// Rebase: edits since the last jump happened at the current stop, so
	// every later stop boundary shifts by the buffer growth.
	if d := m.bufSize() - s.baseSize; d != 0 {
		cur := s.stops[s.idx].end
		for i := range s.stops {
			if s.stops[i].start > cur || (s.stops[i].start == cur && i > s.idx) {
				s.stops[i].start += d
			}
			if s.stops[i].end > cur || (s.stops[i].end == cur && i > s.idx) {
				s.stops[i].end += d
			}
		}
		s.baseSize = m.bufSize()
	}
	ni := s.idx + delta
	if ni < 0 {
		ni = 0
	}
	if ni >= len(s.stops) {
		ni = len(s.stops) - 1
		m.snippetJumpTo(ni)
		m.snippet = nil
		return
	}
	s.idx = ni
	m.snippetJumpTo(ni)
}

// snippetSelActive reports whether the current stop's placeholder text is
// still pre-selected (#2146): set at the jump and only meaningful while the
// cursor still sits at the selection end — any motion or edit moves the
// cursor off selTo and the selection silently lapses.
func (m Model) snippetSelActive() bool {
	s := m.snippet
	return s != nil && s.selActive && m.cursor == s.selTo
}

// snippetSelAt reports whether a cell lies inside the pre-selected
// placeholder span, for the render highlight.
func (m Model) snippetSelAt(line, col int) bool {
	if !m.snippetSelActive() {
		return false
	}
	p := buffer.Position{Line: line, Col: col}
	return !p.Before(m.snippet.selFrom) && p.Before(m.snippet.selTo)
}

// snippetReplaceSelection deletes the pre-selected placeholder text through
// the insert recorder — the replace-on-type half of #2146; the caller inserts
// the typed text at the resulting cursor right after. The current stop's end
// pulls back to its start so a later shift+tab returns before the replacement
// text, exactly like a bare stop.
func (m *Model) snippetReplaceSelection() {
	s := m.snippet
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	m.cursor = m.insert.rec.Apply(buffer.Edit{Range: buffer.Range{Start: s.selFrom, End: s.selTo}})
	m.desiredCol = m.cursor.Col
	s.stops[s.idx].end = s.stops[s.idx].start
	s.selActive = false
	m.dirtyFromInsert()
}

// snippetEnd drops the session without moving the cursor.
func (m *Model) snippetEnd() { m.snippet = nil }

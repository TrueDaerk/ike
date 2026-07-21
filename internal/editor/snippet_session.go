package editor

// snippet_session.go is the tabstop session behind an accepted snippet
// completion (#846): after internal/lsp/snippet expands the insert text, the
// editor jumps the cursor between the expansion's tabstops with tab/shift+tab.
// Stops are absolute rune offsets into the buffer; typing between jumps is
// assumed to happen at the current stop, so on each jump the buffer-size delta
// since the last one shifts every later stop (the sequential fill-in shape).
// Esc (leaving insert mode) ends the session.
type snippetSession struct {
	stops    []int // absolute buffer rune offsets, in visit order
	idx      int
	baseSize int // buffer rune size at the last rebase
}

// startSnippetSession installs the session for text just inserted ending at
// the cursor: rel are tabstop offsets relative to the insert start. A single
// trailing stop is pointless (the cursor already sits there), so it starts no
// session.
func (m *Model) startSnippetSession(text string, rel []int) {
	n := len([]rune(text))
	startOff := m.posToOffset(m.cursor) - n
	if len(rel) == 1 && rel[0] == n {
		return
	}
	s := &snippetSession{stops: make([]int, len(rel)), baseSize: m.bufSize()}
	for i, r := range rel {
		s.stops[i] = startOff + r
	}
	m.snippet = s
	m.cursor = m.buf.Clamp(m.offsetToPos(s.stops[0]))
	m.desiredCol = m.cursor.Col
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
	// every later stop shifts by the buffer growth.
	if d := m.bufSize() - s.baseSize; d != 0 {
		cur := s.stops[s.idx]
		for i := range s.stops {
			if s.stops[i] > cur || (s.stops[i] == cur && i > s.idx) {
				s.stops[i] += d
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
		m.snippet = nil
	} else {
		s.idx = ni
	}
	m.cursor = m.buf.Clamp(m.offsetToPos(s.stops[ni]))
	m.desiredCol = m.cursor.Col
	m.emit(EventCursorMove)
}

// snippetEnd drops the session without moving the cursor.
func (m *Model) snippetEnd() { m.snippet = nil }

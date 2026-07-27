package httppane

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// selection.go is the response viewer's text selection and copy support
// (#1266): a mouse drag selects across the composed rows exactly like the
// terminal pane does (#227, #951 — click/word/line on single, double and
// triple click), and the selection, the body or the headers go to the system
// clipboard. The pane cannot reach the clipboard itself; it asks the host via
// CopyMsg, mirroring how the other tool panes emit messages instead of
// touching globals.

// CopyMsg asks the host to put Text on the system clipboard. What names the
// copied thing for the confirmation notice ("selection", "response body", …).
type CopyMsg struct {
	Text string
	What string
}

// CancelMsg asks the host to abort the in-flight dispatch (#1272); the pane
// knows a request is running but not how to stop it.
type CancelMsg struct{}

// pos is a caret position in the composed view: a row index plus a rune
// column into that row's text.
type pos struct {
	row, col int
}

// before reports whether p sorts ahead of q in reading order.
func (p pos) before(q pos) bool {
	if p.row != q.row {
		return p.row < q.row
	}
	return p.col < q.col
}

// selMode is the unit a drag extends by, set by the click streak.
type selMode int

const (
	selChar selMode = iota
	selWord
	selLine
)

// multiClickWindow is how quickly a follow-up press on the same cell must
// arrive to grow the click streak — the terminal pane's value (#951).
const multiClickWindow = 500 * time.Millisecond

// selection is the mouse-selection state of the viewer.
type selection struct {
	on           bool
	anchor, head pos
	mode         selMode
	// originA/originB delimit the unit the streak started on, so extending a
	// word/line selection never eats into it.
	originA, originB pos

	dragging    bool
	clickStreak int
	lastAt      time.Time
	lastPos     pos
}

// MousePress anchors a selection at the pane-local cell (x, y). The click
// streak cycles char → word → line like the terminal's.
func (m *Model) MousePress(x, y int) {
	p := m.posAt(x, y)
	now := timeNow()
	if p == m.sel.lastPos && now.Sub(m.sel.lastAt) <= multiClickWindow {
		m.sel.clickStreak++
	} else {
		m.sel.clickStreak = 1
	}
	m.sel.lastAt, m.sel.lastPos = now, p
	m.sel.dragging = true
	m.sel.on = false

	switch (m.sel.clickStreak-1)%3 + 1 {
	case 2:
		m.sel.mode = selWord
		a, b, ok := m.wordSpanAt(p)
		if !ok {
			m.sel.mode = selChar
			m.sel.anchor, m.sel.head = p, p
			return
		}
		m.sel.originA, m.sel.originB = a, b
		m.sel.anchor, m.sel.head, m.sel.on = a, b, true
	case 3:
		m.sel.mode = selLine
		a, b := m.lineSpanAt(p.row)
		m.sel.originA, m.sel.originB = a, b
		m.sel.anchor, m.sel.head, m.sel.on = a, b, true
	default:
		m.sel.mode = selChar
		m.sel.anchor, m.sel.head = p, p
	}
}

// MouseDrag extends the selection to (x, y) in the unit the press chose.
func (m *Model) MouseDrag(x, y int) {
	if !m.sel.dragging {
		return
	}
	p := m.posAt(x, y)
	switch m.sel.mode {
	case selWord:
		a, b, ok := m.wordSpanAt(p)
		if !ok {
			a, b = p, pos{row: p.row, col: p.col + 1}
		}
		m.extendUnit(a, b)
	case selLine:
		a, b := m.lineSpanAt(p.row)
		m.extendUnit(a, b)
	default:
		m.sel.head = p
		m.sel.on = m.sel.head != m.sel.anchor
	}
}

// MouseRelease ends the drag; the selection stays visible until it is copied
// or a new press lands.
func (m *Model) MouseRelease() { m.sel.dragging = false }

// extendUnit grows a word/line selection to cover [a, b) without ever
// shrinking below the unit the streak started on (#951).
func (m *Model) extendUnit(a, b pos) {
	switch {
	case a.before(m.sel.originA):
		m.sel.anchor, m.sel.head = m.sel.originB, a
	case m.sel.originB.before(b):
		m.sel.anchor, m.sel.head = m.sel.originA, b
	default:
		m.sel.anchor, m.sel.head = m.sel.originA, m.sel.originB
	}
	m.sel.on = m.sel.anchor != m.sel.head
}

// HasSelection reports whether a selection exists.
func (m *Model) HasSelection() bool { return m.sel.on }

// ClearSelection drops the selection and any drag in progress.
func (m *Model) ClearSelection() { m.sel = selection{} }

// SelectionText extracts the selected text of the composed view: from the
// earlier endpoint (inclusive) to the later one (exclusive), rows joined by
// newlines — what the pane shows, so a pretty-printed JSON body copies as it
// reads on screen.
func (m *Model) SelectionText() string {
	if !m.sel.on {
		return ""
	}
	start, end := m.sel.anchor, m.sel.head
	if end.before(start) {
		start, end = end, start
	}
	var b strings.Builder
	for r := start.row; r <= end.row && r < len(m.rows); r++ {
		if r < 0 {
			continue
		}
		text := []rune(m.rows[r].text)
		from, to := 0, len(text)
		if r == start.row {
			from = min(start.col, to)
		}
		if r == end.row {
			to = min(end.col, to)
		}
		if from > to {
			from = to
		}
		b.WriteString(string(text[from:to]))
		if r < end.row {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// posAt maps a pane-local cell onto a position in the composed rows. The
// content starts one line below the title bar and one column right of the
// leading gutter space every row renders with.
func (m *Model) posAt(x, y int) pos {
	row := m.top + y - 1 // the title line occupies y == 0
	if row < 0 {
		row = 0
	}
	if row > len(m.rows) {
		row = len(m.rows)
	}
	col := x - 1 + m.left // the leading space, plus the horizontal pan (#1290)
	if col < 0 {
		col = 0
	}
	if row < len(m.rows) {
		if n := len([]rune(m.rows[row].text)); col > n {
			col = n
		}
	}
	return pos{row: row, col: col}
}

// isWordRune reports whether r belongs to a word for double-click selection.
// The set is deliberately wide: response bodies are full of ids, tokens and
// URLs that a user double-clicks as one thing.
func isWordRune(r rune) bool {
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
// spans just itself; ok=false on blanks and past the end of a row.
func (m *Model) wordSpanAt(p pos) (a, b pos, ok bool) {
	if p.row < 0 || p.row >= len(m.rows) {
		return a, b, false
	}
	text := []rune(m.rows[p.row].text)
	if p.col >= len(text) {
		return a, b, false
	}
	if !isWordRune(text[p.col]) {
		return pos{p.row, p.col}, pos{p.row, p.col + 1}, true
	}
	start, endCol := p.col, p.col+1
	for start > 0 && isWordRune(text[start-1]) {
		start--
	}
	for endCol < len(text) && isWordRune(text[endCol]) {
		endCol++
	}
	return pos{p.row, start}, pos{p.row, endCol}, true
}

// lineSpanAt returns the span covering the whole row.
func (m *Model) lineSpanAt(row int) (a, b pos) {
	if row < 0 {
		row = 0
	}
	if row >= len(m.rows) {
		return pos{row, 0}, pos{row, 0}
	}
	return pos{row, 0}, pos{row, len([]rune(m.rows[row].text))}
}

// selState reports whether column col of row is inside the selection.
func (m *Model) selState(row, col int) bool {
	if !m.sel.on {
		return false
	}
	start, end := m.sel.anchor, m.sel.head
	if end.before(start) {
		start, end = end, start
	}
	p := pos{row, col}
	return !p.before(start) && p.before(end)
}

// BodyText returns the response body as shown — pretty-printed and without
// the status line or headers (#1266).
func (m *Model) BodyText() string {
	var lines []string
	for _, r := range m.rows {
		if r.kind == kindBody {
			lines = append(lines, r.text)
		}
	}
	return strings.Join(lines, "\n")
}

// HeadersText returns the status line plus the headers as shown, the block a
// user pastes into a bug report.
func (m *Model) HeadersText() string {
	var lines []string
	for _, r := range m.rows {
		switch r.kind {
		case kindStatus, kindHeader:
			lines = append(lines, r.text)
		case kindBody:
			return strings.Join(lines, "\n")
		}
	}
	return strings.Join(lines, "\n")
}

// copyCmd wraps text in a CopyMsg command, or nil when there is nothing to
// copy — an empty clipboard write would silently destroy what the user had.
func copyCmd(text, what string) tea.Cmd {
	if text == "" {
		return nil
	}
	return func() tea.Msg { return CopyMsg{Text: text, What: what} }
}

// timeNow is a seam so click-streak tests are not wall-clock dependent.
var timeNow = time.Now

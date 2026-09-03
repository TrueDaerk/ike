package terminal

// copymode.go — tmux-style copy mode (#2162): a reserved chord (cmd+shift+c,
// app-side like cmd+f) detaches the keyboard from the PTY and moves a cursor
// over the whole virtual buffer [scrollback ++ screen] with vim motions
// (hjkl, w/b, 0/$, gg/G, ctrl+u/d). `v` anchors a selection (the same
// anchor/head fields the mouse selection renders and extracts through), `y`
// yanks it to the system clipboard via the app's clipboard funnel (CopiedMsg)
// and exits, esc/q leave back to raw mode with the view snapped to live.
//
// Scrollback search inside the mode: `/` (forward, toward newer) and `?`
// (backward, toward older) open a query line on the status row; enter jumps
// to the nearest match in the search's direction, n/N repeat/reverse it,
// both wrapping around; visible matches highlight reverse-video and a miss
// reports `no matches`. Matching is the scrollback search's rule (#1169):
// case-insensitive contains over the plain line text.
//
// The mode owns the keyboard completely — every key is consumed, nothing
// leaks into the shell — while PTY output keeps flowing into the emulator
// untouched: the session's feed loop never pauses, so nothing printed during
// copy mode is lost. The view anchors on an absolute virtual line (top), not
// the live-relative scroll offset, so arriving output cannot slide the
// window (or the cursor) away from the text being selected.

import (
	"image/color"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/ui"
)

// CopiedMsg carries a copy-mode yank (#2162) to the app, whose clipboard
// funnel writes the system clipboard and records the clipboard history —
// the same path every pane copy takes (#2061).
type CopiedMsg struct {
	Key  string // session key, for routing symmetry with OutputMsg
	Text string
}

// copyMode is the open copy mode: the cursor and window in virtual
// coordinates, the pending selection anchor, and the search state.
type copyMode struct {
	cur      vpos // cursor over [scrollback ++ screen]
	top      int  // virtual line rendered at the window's first row
	selting  bool // v pressed: the selection follows the cursor
	selStart vpos // selection anchor (inclusive) while selting
	pendingG bool // first g of a gg chord seen

	// Search: input is the open query line (`/` or `?` typed), back its
	// direction (true = ? = toward older lines); accepting moves the query
	// to last/lastBack, which drive n/N and the highlights.
	input    bool
	back     bool
	query    ui.Field
	last     string
	lastBack bool
	miss     bool // the last jump found no match
	matchIdx int  // 1-based index of the current match, 0 without one
	matchCnt int
	wrapped  bool // the last jump came back around the scrollback (#2410)
}

// Copying reports whether copy mode is open.
func (m Model) Copying() bool { return m.copy != nil }

// StartCopyMode enters copy mode (#2162) from the app-side chord. Like the
// scrollback search it refuses under a live alt-screen or mouse-reporting
// child (vim/lazygit own their keys); a finished session (#1951) always
// qualifies — its buffer is exactly what copy mode reads. Already open, it
// reports true without resetting.
func (m *Model) StartCopyMode() bool {
	if m.sess == nil {
		return false
	}
	if !m.dead() && (m.sess.AltScreen() || m.sess.WantsMouse()) {
		return false
	}
	if m.copy != nil {
		return true
	}
	m.search = nil
	m.hints = nil
	m.ClearSelection()
	sb := m.sess.ScrollbackLen()
	top := clamp(sb-m.scroll, 0, m.copyTopMax())
	// The cursor starts on the window's bottom content row — where the
	// prompt (or the newest scrolled-in line) sits.
	cur := clamp(top+m.copyRows()-1, 0, m.copyTotal()-1)
	m.copy = &copyMode{top: top, cur: vpos{line: cur}}
	return true
}

// exitCopyMode leaves copy mode back to raw routing; the view snaps to live.
func (m *Model) exitCopyMode() {
	m.copy = nil
	m.ClearSelection()
	m.scroll = 0
}

// copyTotal is the virtual line count: scrollback plus the screen rows.
func (m Model) copyTotal() int {
	return m.sess.ScrollbackLen() + m.h
}

// copyRows is the window's content height: the pane minus the status row.
func (m Model) copyRows() int {
	if m.h > 1 {
		return m.h - 1
	}
	return 1
}

// copyTopMax is the largest window top that still fills the content rows.
func (m Model) copyTopMax() int {
	if max := m.copyTotal() - m.copyRows(); max > 0 {
		return max
	}
	return 0
}

// copyKey routes one key while copy mode is open. Every key is consumed —
// nothing falls through to the PTY. The returned command is the yank's
// CopiedMsg, nil otherwise.
func (m *Model) copyKey(msg tea.KeyPressMsg) tea.Cmd {
	c := m.copy
	key := msg.String()
	if delta, ok := ui.MatchStepChord(key); ok && c.last != "" {
		// The shared match-step chord (#2410) steps the accepted search from
		// anywhere in the mode — the open query line included, which is the
		// whole point: n / N are query text while it is up.
		m.copyStepMatch(delta)
		return nil
	}
	if c.input {
		m.copySearchKey(msg)
		return nil
	}
	if c.pendingG {
		c.pendingG = false
		if key == "g" {
			c.cur = vpos{line: 0}
			m.copyAfterMove()
		}
		return nil
	}
	c.miss = false
	if ui.FindChord(key) {
		// Copy mode has the keyboard detached from the PTY, so the shared
		// find chord (#2409) reaches it directly, alongside the bare key.
		m.openCopySearch()
		return nil
	}
	switch key {
	case "esc":
		if c.selting {
			c.selting = false
			m.ClearSelection()
			return nil
		}
		m.exitCopyMode()
	case "q":
		m.exitCopyMode()
	case "v":
		if c.selting {
			c.selting = false
			m.ClearSelection()
			return nil
		}
		c.selting = true
		c.selStart = c.cur
		m.copySyncSelection()
	case "y":
		return m.copyYank()
	case "h", "left":
		m.copyMove(0, -1)
	case "l", "right":
		m.copyMove(0, 1)
	case "j", "down":
		m.copyMove(1, 0)
	case "k", "up":
		m.copyMove(-1, 0)
	case "0", "home":
		c.cur.col = 0
		m.copyAfterMove()
	case "$", "end":
		c.cur.col = m.copyLineEnd(c.cur.line)
		m.copyAfterMove()
	case "w":
		m.copyWordFwd()
	case "b":
		m.copyWordBack()
	case "g":
		c.pendingG = true
	case "G":
		c.cur = vpos{line: m.copyTotal() - 1}
		m.copyAfterMove()
	case "ctrl+u", "pgup":
		m.copyMove(-m.pageSize(), 0)
	case "ctrl+d", "pgdown":
		m.copyMove(m.pageSize(), 0)
	case "/":
		m.openCopySearch()
	case "?":
		c.input, c.back = true, true
		c.query.Clear()
	case "n":
		m.copySearchStep(false)
	case "N":
		m.copySearchStep(true)
	}
	return nil
}

// openCopySearch puts the cursor in copy mode's forward search prompt.
func (m *Model) openCopySearch() {
	c := m.copy
	c.input, c.back = true, false
	c.query.Clear()
}

// copyYank extracts the selection, exits copy mode and hands the text to the
// app's clipboard funnel; a bare y without a selection is inert.
func (m *Model) copyYank() tea.Cmd {
	if !m.copy.selting {
		return nil
	}
	text := m.SelectionText()
	key := m.SessionKey()
	m.exitCopyMode()
	if text == "" {
		return nil
	}
	return func() tea.Msg { return CopiedMsg{Key: key, Text: text} }
}

// copyMove steps the cursor by (dl, dc), clamped to the buffer and grid.
func (m *Model) copyMove(dl, dc int) {
	c := m.copy
	c.cur.line = clamp(c.cur.line+dl, 0, m.copyTotal()-1)
	maxCol := m.gridW() - 1
	if maxCol < 0 {
		maxCol = 0
	}
	c.cur.col = clamp(c.cur.col+dc, 0, maxCol)
	m.copyAfterMove()
}

// copyAfterMove re-anchors the window on the cursor, re-syncs the selection
// and mirrors the position into the scroll offset (the scrollbar reads it).
func (m *Model) copyAfterMove() {
	c := m.copy
	rows := m.copyRows()
	if c.cur.line < c.top {
		c.top = c.cur.line
	}
	if c.cur.line > c.top+rows-1 {
		c.top = c.cur.line - rows + 1
	}
	c.top = clamp(c.top, 0, m.copyTopMax())
	m.copySyncSelection()
	sb := m.sess.ScrollbackLen()
	m.scroll = clamp(sb-c.top, 0, sb)
}

// copySyncSelection projects the inclusive copy-mode span onto the model's
// anchor/head selection fields (exclusive head), so the mouse selection's
// highlight and SelectionText serve copy mode unchanged.
func (m *Model) copySyncSelection() {
	c := m.copy
	if !c.selting {
		return
	}
	start, end := c.selStart, c.cur
	if end.before(start) {
		start, end = end, start
	}
	m.selAnchor = start
	m.selHead = vpos{line: end.line, col: end.col + 1}
	m.selOn = true
	m.dragging = false
}

// copyLineEnd is the last content column of virtual line v (0 when blank).
func (m Model) copyLineEnd(v int) int {
	if n := utf8.RuneCountInString(m.sess.LineText(v)); n > 0 {
		return n - 1
	}
	return 0
}

// copyWordFwd moves to the start of the next word — whitespace-separated
// runs, crossing line ends — stopping at the buffer's last line end.
func (m *Model) copyWordFwd() {
	c := m.copy
	total := m.copyTotal()
	line := c.cur.line
	text := []rune(m.sess.LineText(line))
	i := c.cur.col
	for i < len(text) && text[i] != ' ' {
		i++
	}
	for {
		for i < len(text) && text[i] == ' ' {
			i++
		}
		if i < len(text) {
			c.cur = vpos{line: line, col: i}
			break
		}
		if line >= total-1 {
			c.cur = vpos{line: line, col: m.copyLineEnd(line)}
			break
		}
		line++
		text = []rune(m.sess.LineText(line))
		i = 0
	}
	m.copyAfterMove()
}

// copyWordBack moves to the start of the previous word, crossing line
// starts, stopping at the buffer's first column.
func (m *Model) copyWordBack() {
	c := m.copy
	line := c.cur.line
	text := []rune(m.sess.LineText(line))
	i := c.cur.col - 1
	for {
		for i >= 0 && (i >= len(text) || text[i] == ' ') {
			i--
		}
		if i >= 0 {
			for i > 0 && text[i-1] != ' ' {
				i--
			}
			c.cur = vpos{line: line, col: i}
			break
		}
		if line == 0 {
			c.cur = vpos{line: 0}
			break
		}
		line--
		text = []rune(m.sess.LineText(line))
		i = len(text) - 1
	}
	m.copyAfterMove()
}

// copySearchKey feeds one key to the open query line: enter accepts and
// jumps, esc cancels, everything else edits via the shared field editing.
func (m *Model) copySearchKey(msg tea.KeyPressMsg) {
	c := m.copy
	switch msg.Code {
	case tea.KeyEscape:
		c.input = false
		c.query.Clear()
		return
	case tea.KeyEnter:
		c.input = false
		if !c.query.Empty() {
			c.last, c.lastBack = c.query.Text, c.back
			m.copySearchJump(c.back)
		}
		c.query.Clear()
		return
	}
	c.query.Key(msg)
}

// copyMatches returns the virtual lines containing the accepted query,
// ascending — the scrollback search's matching rule (#1169).
func (m Model) copyMatches() []int {
	c := m.copy
	if c == nil || c.last == "" {
		return nil
	}
	q := strings.ToLower(c.last)
	var out []int
	for v, total := 0, m.copyTotal(); v < total; v++ {
		if strings.Contains(strings.ToLower(m.sess.LineText(v)), q) {
			out = append(out, v)
		}
	}
	return out
}

// copySearchJump moves to the nearest match strictly past the cursor line in
// the given direction (back = toward older lines), wrapping around; the
// cursor lands on the match's first occurrence column. No match anywhere
// sets the miss flag the status row reports.
func (m *Model) copySearchJump(back bool) {
	c := m.copy
	matches := m.copyMatches()
	c.matchIdx, c.matchCnt = 0, len(matches)
	if len(matches) == 0 {
		c.miss = true
		return
	}
	dir := 1
	if back {
		dir = -1
	}
	pick, idx, wrapped, _ := ui.StepSorted(matches, c.cur.line, dir)
	c.matchIdx, c.wrapped = idx+1, wrapped
	col := 0
	if i := strings.Index(strings.ToLower(m.sess.LineText(pick)), strings.ToLower(c.last)); i > 0 {
		col = utf8.RuneCountInString(m.sess.LineText(pick)[:i])
	}
	c.cur = vpos{line: pick, col: col}
	m.copyAfterMove()
}

// copySearchStep repeats the accepted search: n keeps its direction,
// N (reverse) flips it.
func (m *Model) copySearchStep(reverse bool) {
	c := m.copy
	if c.last == "" {
		return
	}
	m.copySearchJump(c.lastBack != reverse)
}

// copyStepMatch is copySearchStep in the shape the shared cmd+g chord reports
// (#2410): delta 1 walks the accepted search in its own direction, -1 against
// it — the same n / N meaning, reachable while the query line is still open.
// Without an accepted query the chord is not ours.
func (m *Model) copyStepMatch(delta int) ui.MatchStep {
	c := m.copy
	if c == nil || c.last == "" {
		return ui.NoStep
	}
	m.copySearchJump(c.lastBack != (delta < 0))
	if c.matchCnt == 0 {
		return ui.NoMatches()
	}
	return ui.Stepped(c.matchIdx-1, c.matchCnt, c.wrapped)
}

// copyView renders copy mode: the windowed buffer rows with selection and
// match highlights, the cursor cell reversed, and the status row last.
func (m Model) copyView() string {
	c := m.copy
	// A one-row pane has no room for content next to the status row.
	if m.h <= 1 {
		return m.copyStatusLine()
	}
	sb := m.sess.ScrollbackLen()
	total := sb + m.h
	rows := m.copyRows()
	top := clamp(c.top, 0, m.copyTopMax())
	screen := strings.Split(m.sess.View(), "\n")
	out := make([]string, 0, m.h)
	for i := 0; i < rows; i++ {
		v := top + i
		switch {
		case v >= total:
			out = append(out, "")
		case v < sb:
			// Scrollback rows decorate as they window in (#1168), like the
			// paged view.
			out = append(out, decorateLinkLine(m.sess.HistoryLine(v)))
		case v-sb < len(screen):
			out = append(out, screen[v-sb])
		default:
			out = append(out, "")
		}
	}
	m.highlightSelection(out, top)
	m.copyHighlightMatches(out, top)
	if cr := c.cur.line - top; cr >= 0 && cr < len(out) {
		out[cr] = reverseCell(out[cr], c.cur.col)
	}
	out = append(out, m.copyStatusLine())
	return strings.Join(out, "\n")
}

// copyHighlightMatches reverse-videos every occurrence of the active query —
// the one being typed, else the accepted one — on the visible rows.
func (m Model) copyHighlightMatches(rows []string, firstVirtual int) {
	c := m.copy
	q := c.last
	if c.input {
		q = c.query.Text
	}
	if q == "" {
		return
	}
	q = strings.ToLower(q)
	for i := range rows {
		text := strings.ToLower(m.sess.LineText(firstVirtual + i))
		off := 0
		for {
			idx := strings.Index(text[off:], q)
			if idx < 0 {
				break
			}
			from := utf8.RuneCountInString(text[:off+idx])
			to := from + utf8.RuneCountInString(q)
			rows[i] = reverseSpan(rows[i], from, to)
			off += idx + len(q)
		}
	}
}

// copyStatusLine renders the mode indicator row: the COPY badge, the cursor's
// line position, and the open query line or the last search's outcome.
func (m Model) copyStatusLine() string {
	c := m.copy
	var accent color.Color = lipgloss.White
	var errCol color.Color = lipgloss.Red
	var dimCol color.Color = lipgloss.Color("245")
	if m.pal != nil {
		accent, errCol, dimCol = m.pal.Accent, m.pal.Error, m.pal.InlayHint
	}
	badge := lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(accent).
		Render(" COPY ")
	pos := lipgloss.NewStyle().Foreground(dimCol).
		Render(" " + strconv.Itoa(c.cur.line+1) + "/" + strconv.Itoa(m.copyTotal()))
	tail := ""
	switch {
	case c.input:
		prefix := "/"
		if c.back {
			prefix = "?"
		}
		tail = "  " + prefix + c.query.View()
	case c.miss:
		tail = lipgloss.NewStyle().Foreground(errCol).Render("  no matches")
	case c.last != "" && c.matchCnt > 0:
		tail = lipgloss.NewStyle().Foreground(dimCol).
			Render("  /" + c.last + " " + ui.MatchCounter(c.matchIdx, c.matchCnt, c.wrapped))
	case c.selting:
		tail = lipgloss.NewStyle().Foreground(dimCol).Render("  VISUAL — y yanks")
	}
	w := m.w
	if w < 1 {
		w = 1
	}
	return ansi.Truncate(badge+pos+tail, w, "…")
}

// copyWheel pages the copy-mode window by delta lines (positive = older),
// pulling the cursor along so it stays visible.
func (m *Model) copyWheel(delta int) {
	c := m.copy
	c.top = clamp(c.top-delta, 0, m.copyTopMax())
	rows := m.copyRows()
	c.cur.line = clamp(c.cur.line, c.top, c.top+rows-1)
	m.copyAfterMove()
}

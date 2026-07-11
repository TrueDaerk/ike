package editor

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/search"
)

// MouseClick moves the cursor to the content-local cell (x, y) — coordinates
// relative to the editor's content area (gutter included, title/border excluded).
// It maps the click through the gutter width and the current scroll offsets. In
// insert/replace mode the cursor may land one past the line end; otherwise it
// snaps onto a character.
func (m *Model) MouseClick(x, y int) {
	// A click dismisses cursor-anchored popups like any keypress does (#307):
	// the popup anchors at the cursor, so leaving it open would make it trail
	// every click/drag. The server re-opens signature help on the next
	// keystroke inside the call.
	m.dismissHover()
	m.dismissSignature()
	if y < 0 {
		y = 0
	}
	if x < 0 {
		x = 0
	}
	line := m.view.Top + y
	if line > m.buf.LineCount()-1 {
		line = m.buf.LineCount() - 1
	}
	col := x - m.view.GutterWidth(m.buf.LineCount()) + m.view.Left
	if col < 0 {
		col = 0
	}
	p := buffer.Position{Line: line, Col: col}
	if m.mode == Insert || m.mode == Replace {
		m.cursor = m.buf.Clamp(p)
	} else {
		m.cursor = m.buf.ClampCursor(p)
	}
	m.desiredCol = m.cursor.Col
	m.scroll()
	m.emit(EventCursorMove)
}

// ScrollBy moves the viewport by delta lines (positive down, negative up)
// without moving the cursor, clamped to the buffer — a mouse-wheel scroll,
// independent of mode. Vertical only; see ScrollXBy for horizontal.
func (m *Model) ScrollBy(delta int) {
	m.SetScroll(m.view.Top+delta, m.view.Left)
}

// ScrollXBy moves the viewport by delta columns (positive right) without moving
// the cursor — a horizontal-wheel or shift+wheel scroll (#230). It clamps so at
// least the last character of the longest visible line stays on screen; the
// next cursor motion re-derives the offset to follow the cursor again.
func (m *Model) ScrollXBy(delta int) {
	maxLen := 0
	for i := m.view.Top; i < m.view.Bottom(m.buf.LineCount()); i++ {
		if n := len([]rune(m.buf.Line(i))); n > maxLen {
			maxLen = n
		}
	}
	left := m.view.Left + delta
	if max := maxLen - 1; left > max {
		left = max
	}
	if left < 0 {
		left = 0
	}
	m.view.Left = left
}

// CommandLine returns the text shown on the command line: ":cmd" in ex mode or
// "/pat" / "?pat" while searching. It is "" outside command mode.
func (m Model) CommandLine() string {
	if m.mode != Command {
		return ""
	}
	if m.searching {
		if m.searchDir == 0 { // search.Forward
			return "/" + m.cmdline
		}
		return "?" + m.cmdline
	}
	return ":" + m.cmdline
}

// View renders the buffer: a line-number gutter (when enabled), horizontally and
// vertically scrolled text, and the cursor cell highlighted when focused.
func (m Model) View() string {
	if m.path == "" && m.buf.LineCount() == 1 && m.buf.Line(0) == "" {
		// The command line must show even on the empty scratch buffer (":q").
		if cl := m.commandLineRow(); cl != "" {
			return cl
		}
		return lipgloss.NewStyle().Faint(true).Render("(no file open)")
	}
	lineCount := m.buf.LineCount()
	bottom := m.view.Bottom(lineCount)
	gutterStyle := lipgloss.NewStyle().Faint(true)
	cursorStyle := lipgloss.NewStyle().Reverse(true)
	textWidth := m.view.TextWidth(lineCount)

	selStyle := lipgloss.NewStyle().Background(m.theme().SelectionMuted)

	var out []string
	for i := m.view.Top; i < bottom; i++ {
		gs := gutterStyle
		// Colour the gutter for a line carrying diagnostics (red error / yellow warn),
		// the cheap sign-column indicator that keeps the gutter width unchanged.
		if sev, ok := m.worstSeverityOnLine(i); ok {
			gs = lipgloss.NewStyle().Foreground(m.diagColor(sev))
		}
		gutter := gs.Render(m.view.Gutter(i, m.cursor.Line, lineCount))
		body := m.renderLine(i, textWidth, cursorStyle, selStyle)
		out = append(out, gutter+body)
	}
	// An open find/replace panel (#283) renders as the pane's bottom rows;
	// otherwise an active ":" / "/" / "?" input renders as the bottom row
	// (vim-style). Short files pad down so the rows sit at the bottom.
	if rows := m.replacePanelRows(textWidth + m.view.GutterWidth(lineCount)); len(rows) > 0 {
		h := m.view.Height()
		if h < len(rows) {
			h = len(rows)
		}
		if len(out) > h-len(rows) {
			out = out[:h-len(rows)]
		}
		for len(out) < h-len(rows) {
			out = append(out, "")
		}
		out = append(out, rows...)
	} else if cl := m.commandLineRow(); cl != "" {
		h := m.view.Height()
		if h < 1 {
			h = 1
		}
		if len(out) >= h {
			out = out[:h-1]
		}
		for len(out) < h-1 {
			out = append(out, "")
		}
		out = append(out, cl)
	}
	if len(out) == 0 {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Left, out...)
}

// commandLineRow renders the active command-line input with a block cursor, or
// the last ex-command message while idle, or "" when neither is present. While
// searching, a live match counter ("3/17", "no matches") trails the input.
func (m Model) commandLineRow() string {
	if m.subConfirm != nil {
		return m.cmdMsg // the "replace (y/n/a/q/l)?" prompt
	}
	cl := m.CommandLine()
	if cl == "" {
		if m.mode != Command && m.cmdMsg != "" {
			return m.cmdMsg
		}
		return ""
	}
	return cl + lipgloss.NewStyle().Reverse(true).Render(" ") + m.searchCounter()
}

// searchCounter renders the incremental-search tally for the command-line row
// (#255): current match index over total, or "no matches" for a pattern that
// hits nothing. Empty outside an active search line or on an empty pattern.
func (m Model) searchCounter() string {
	if !m.searching || m.preview.Empty() {
		return ""
	}
	all := m.preview.AllMatches(m.buf)
	dim := lipgloss.NewStyle().Faint(true)
	if len(all) == 0 {
		return dim.Render("  no matches")
	}
	cur := 0
	for i, s := range all {
		if s.Line == m.cursor.Line && s.Start == m.cursor.Col {
			cur = i + 1
			break
		}
	}
	return dim.Render("  " + strconv.Itoa(cur) + "/" + strconv.Itoa(len(all)))
}

// searchHLQuery returns the query whose matches the view highlights: the live
// preview while the search line or the replace panel (#283) is open, else the
// committed query while highlights are armed (until a normal-mode Esc clears
// them, #255).
func (m Model) searchHLQuery() (search.Query, bool) {
	if (m.searching || m.replPanel != nil) && !m.preview.Empty() {
		return m.preview, true
	}
	if m.hlActive && !m.query.Empty() {
		return m.query, true
	}
	return search.Query{}, false
}

// renderLine renders one buffer line within the horizontal window, overlaying
// the visual selection and the cursor cell (the cursor wins on overlap). It
// budgets by display cells, not runes: a tab expands to tabWidth spaces so the
// rendered width matches what the terminal shows, which keeps the line inside its
// pane (a raw tab would otherwise be expanded by the terminal past the budget and
// wrap, pushing the pane's bottom border off screen). It stops at the end of
// meaningful content so trailing blanks are not emitted.
func (m Model) renderLine(line, width int, cursorStyle, selStyle lipgloss.Style) string {
	runes := []rune(m.buf.Line(line))
	left := m.view.Left
	selStart, selEnd, hasSel := m.selectionOnLine(line, len(runes))
	isCursorLine := line == m.cursor.Line && m.focused

	// Search-match highlighting (#255): all matches of the active query get a
	// background; the span the cursor sits on (the current match) is also
	// underlined so it stands apart from the rest.
	var matchSpans []search.Span
	if q, ok := m.searchHLQuery(); ok {
		matchSpans = q.LineMatches(m.buf, line)
	}
	matchStyle := lipgloss.NewStyle().Background(m.theme().SelectionMuted)
	curMatchStyle := matchStyle.Underline(true)

	var b strings.Builder
	disp := 0 // display cells emitted so far
	for col := left; disp < width; col++ {
		cursorHere := isCursorLine && col == m.cursor.Col
		selected := hasSel && col >= selStart && col <= selEnd
		if col >= len(runes) && !cursorHere && !selected {
			break // nothing meaningful left on this line
		}

		cell, cells := " ", 1
		if col < len(runes) {
			if runes[col] == '\t' {
				cell, cells = strings.Repeat(" ", m.tabWidth), m.tabWidth
			} else {
				cell = string(runes[col])
			}
		}
		if disp+cells > width { // clamp a tab straddling the right edge
			cells = width - disp
			cell = strings.Repeat(" ", cells)
		}

		inMatch, inCurrent := false, false
		for _, s := range matchSpans {
			if col >= s.Start && col < s.End {
				inMatch = true
				inCurrent = line == m.cursor.Line && m.cursor.Col >= s.Start && m.cursor.Col < s.End
				break
			}
		}

		switch {
		case cursorHere && cells > 1:
			// Cursor on a tab: highlight only the first cell, leave the rest plain.
			b.WriteString(cursorStyle.Render(" "))
			b.WriteString(strings.Repeat(" ", cells-1))
		case cursorHere:
			b.WriteString(cursorStyle.Render(cell))
		case selected:
			b.WriteString(selStyle.Render(cell))
		case inCurrent:
			b.WriteString(curMatchStyle.Render(cell))
		case inMatch:
			b.WriteString(matchStyle.Render(cell))
		default:
			st, styled := m.styleAt(line, col)
			if sev, ok := m.diagSeverityAt(line, col); ok {
				// Diagnostic underline composes over the syntax colour (syntax base <
				// diagnostic underline); cursor/selection already won above.
				st = st.Underline(true).UnderlineColor(m.diagColor(sev))
				styled = true
			}
			if styled {
				b.WriteString(st.Render(cell))
			} else {
				b.WriteString(cell)
			}
		}
		disp += cells
	}
	return b.String()
}

// selectionOnLine returns the inclusive rune-column range to highlight on line
// for the active visual mode, or ok=false when the line is outside the selection
// or no visual mode is active.
func (m Model) selectionOnLine(line, runeLen int) (start, end int, ok bool) {
	if sc := m.subConfirm; sc != nil {
		// Highlight the current ":s///c" match on its line.
		if line == sc.curLine && sc.curEnd > sc.curStart {
			return sc.curStart, sc.curEnd - 1, true
		}
		return 0, 0, false
	}
	if !m.mode.IsVisual() {
		return 0, 0, false
	}
	switch m.mode {
	case Visual:
		lo, hi := m.anchor, m.cursor
		if hi.Before(lo) {
			lo, hi = hi, lo
		}
		if line < lo.Line || line > hi.Line {
			return 0, 0, false
		}
		start = 0
		if line == lo.Line {
			start = lo.Col
		}
		end = runeLen // through the line break for middle lines
		if line == hi.Line {
			end = hi.Col
		}
		return start, end, true
	default: // VisualLine and VisualBlock
		lo, hi := minInt(m.anchor.Line, m.cursor.Line), maxInt(m.anchor.Line, m.cursor.Line)
		if line < lo || line > hi {
			return 0, 0, false
		}
		if m.mode == VisualBlock {
			return minInt(m.anchor.Col, m.cursor.Col), maxInt(m.anchor.Col, m.cursor.Col), true
		}
		return 0, runeLen, true
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

package editor

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/search"
	"ike/internal/editor/viewport"
	ilsp "ike/internal/lsp"
)

// MouseClick moves the cursor to the content-local cell (x, y) — coordinates
// relative to the editor's content area (gutter included, title/border excluded).
// It maps the click through the gutter width and the current scroll offsets. In
// insert/replace mode the cursor may land one past the line end; otherwise it
// snaps onto a character.
func (m *Model) MouseClick(x, y int) {
	p := m.clickPosition(x, y)
	// A plain click returns to single-caret editing (#145).
	m.collapseCarets()
	m.cursor = p
	m.desiredCol = m.cursor.Col
	m.scroll()
	m.emit(EventCursorMove)
}

// AltClick toggles a secondary caret at the clicked cell (#145): alt+click on
// a caret removes it, anywhere else adds one. The primary caret stays put.
func (m *Model) AltClick(x, y int) {
	m.toggleCaret(m.clickPosition(x, y))
}

// clickPosition maps a content-local cell to a buffer position (see
// MouseClick) and dismisses cursor-anchored popups, like any keypress does
// (#307): the popup anchors at the cursor, so leaving it open would make it
// trail every click/drag. The server re-opens signature help on the next
// keystroke inside the call.
func (m *Model) clickPosition(x, y int) buffer.Position {
	m.dismissHover()
	m.dismissSignature()
	if y < 0 {
		y = 0
	}
	if x < 0 {
		x = 0
	}
	line := m.view.Top + y
	colBase := m.view.Left
	if m.softWrap {
		// Soft wrap (#64): map the clicked row through the wrap segments —
		// the mouse map's inverse of the wrapped View() loop. The column
		// counts from the clicked segment's start; there is no horizontal
		// scroll under wrap.
		line, colBase = m.wrapClickAt(y)
	} else if m.hasFolds() {
		// Collapsed folds render as one row (#144): map the clicked row
		// through the visible lines, mirroring the View() render loop.
		line = m.displayLineAt(y)
	}
	// A click on a pinned sticky-scroll header (#168) jumps to its declaration
	// instead of the buffer line the row covers.
	if sticky := m.stickyLines(); y < len(sticky) {
		line, colBase = sticky[y], 0
		if !m.softWrap {
			colBase = m.view.Left
		}
	}
	if line > m.buf.LineCount()-1 {
		line = m.buf.LineCount() - 1
	}
	col := x - m.view.GutterWidth(m.buf.LineCount()) + colBase
	if m.softWrap && col < colBase {
		col = colBase // a gutter click snaps to the clicked segment's start
	}
	if col < 0 {
		col = 0
	}
	p := buffer.Position{Line: line, Col: col}
	if m.mode == Insert || m.mode == Replace {
		return m.buf.Clamp(p)
	}
	return m.buf.ClampCursor(p)
}

// ScrollBy moves the viewport by delta lines (positive down, negative up)
// without moving the cursor, clamped to the buffer — a mouse-wheel scroll,
// independent of mode. Vertical only; see ScrollXBy for horizontal.
func (m *Model) ScrollBy(delta int) {
	if m.hasFolds() {
		// A collapsed fold scrolls past as a single row (#144).
		top, dir := m.view.Top, 1
		if delta < 0 {
			dir, delta = -1, -delta
		}
		for ; delta > 0; delta-- {
			n, ok := m.visibleStep(top, dir)
			if !ok {
				break
			}
			top = n
		}
		m.SetScroll(top, m.view.Left)
		return
	}
	m.SetScroll(m.view.Top+delta, m.view.Left)
}

// ScrollXBy moves the viewport by delta columns (positive right) without moving
// the cursor — a horizontal-wheel or shift+wheel scroll (#230). It clamps so at
// least the last character of the longest visible line stays on screen; the
// next cursor motion re-derives the offset to follow the cursor again.
func (m *Model) ScrollXBy(delta int) {
	if m.softWrap {
		return // no horizontal scroll under soft wrap (#64)
	}
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
	gutterStyle := lipgloss.NewStyle().Faint(true)
	cursorStyle := lipgloss.NewStyle().Reverse(true)
	textWidth := m.view.TextWidth(lineCount)

	selStyle := lipgloss.NewStyle().Background(m.theme().SelectionMuted)

	var out []string
	// Sticky scroll (#168): the enclosing declaration headers replace the top
	// rows of the pane; the buffer lines they cover are skipped, so the first
	// content row is the line right below the innermost pinned scope header.
	sticky := m.stickyLines()
	for _, line := range sticky {
		gutter := gutterStyle.Render(m.view.Gutter(line, m.cursor.Line, lineCount))
		body := m.renderLine(line, textWidth, cursorStyle, selStyle)
		out = append(out, gutter+body)
	}
	// Body rows: fill the remaining height, skipping lines hidden inside
	// collapsed folds (#144) — a closed fold occupies exactly one row, its
	// header, rendered with a hidden-line-count placeholder.
	height := m.view.Height()
	if height <= 0 {
		// An unsized pane renders every line, matching viewport.Bottom.
		height = lineCount + len(sticky)
	}
	for i := m.view.Top + len(sticky); len(out) < height && i < lineCount; i++ {
		if m.lineHidden(i) {
			continue
		}
		gs := gutterStyle
		// Colour the gutter for a line carrying diagnostics (red error / yellow warn),
		// the cheap sign-column indicator that keeps the gutter width unchanged.
		// A git diff marker (Roadmap 0320, #464) colours the same way; a
		// diagnostic wins the cell when both apply.
		if sev, ok := m.worstSeverityOnLine(i); ok {
			gs = lipgloss.NewStyle().Foreground(m.diagColor(sev))
		} else if mk, ok := m.gitMarks[i]; ok {
			gs = lipgloss.NewStyle().Foreground(m.gitMarkColor(mk))
		}
		gutter := gs.Render(m.view.Gutter(i, m.cursor.Line, lineCount))
		if end, ok := m.foldedAt(i); ok {
			out = append(out, gutter+m.renderFoldHeader(i, end, textWidth, cursorStyle, selStyle))
			continue
		}
		if m.softWrap {
			// Soft wrap (#64): one row per wrap segment; continuation rows
			// carry a wrap marker in the gutter instead of a line number.
			segs := m.wrapSegs(i)
			for si := range segs {
				if len(out) >= height {
					break
				}
				to := -1 // final segment: unbounded, through the content end
				if si+1 < len(segs) {
					to = segs[si+1]
				}
				g := gutter
				if si > 0 {
					g = gutterStyle.Render(m.view.GutterContinuation(lineCount))
				}
				out = append(out, g+m.renderSpan(i, segs[si], to, textWidth, cursorStyle, selStyle))
			}
			continue
		}
		row := m.renderLine(i, textWidth, cursorStyle, selStyle)
		if m.blameOn && i == m.cursor.Line {
			// Inline blame (0320, #468): the annotation splices into the
			// cursor line's right padding when it fits.
			row = m.blameAnnotate(row, i, textWidth)
		}
		out = append(out, gutter+row)
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
// meaningful content so trailing blanks are not emitted (unless a ruler column
// lies past it, which pads with tinted blanks so the ruler stays visible).
func (m Model) renderLine(line, width int, cursorStyle, selStyle lipgloss.Style) string {
	return m.renderSpan(line, m.view.Left, -1, width, cursorStyle, selStyle)
}

// renderSpan renders the columns [from, to) of one buffer line (to < 0 means
// unbounded — through the content end). Under soft wrap (#64) each wrap
// segment is one span; the unwrapped renderLine is the single span starting at
// the horizontal scroll offset. Whitespace glyphs, indent guides, and ruler
// tints (#64) overlay here so both paths share them.
func (m Model) renderSpan(line, from, to, width int, cursorStyle, selStyle lipgloss.Style) string {
	runes := []rune(m.buf.Line(line))
	left := from
	selStart, selEnd, hasSel := m.selectionOnLine(line, len(runes))
	isCursorLine := line == m.cursor.Line && m.focused
	// Secondary carets (#145) render dimmer than the primary cell.
	caretStyle := cursorStyle.Faint(true)

	// Search-match highlighting (#255): all matches of the active query get a
	// background; the span the cursor sits on (the current match) is also
	// underlined so it stands apart from the rest.
	var matchSpans []search.Span
	if q, ok := m.searchHLQuery(); ok {
		matchSpans = q.LineMatches(m.buf, line)
	}
	matchStyle := lipgloss.NewStyle().Background(m.theme().SelectionMuted)
	curMatchStyle := matchStyle.Underline(true)

	// Inlay hints (#171): virtual text injected before the cell it anchors at,
	// dimmed and italic so it never reads as buffer content. Hints scrolled off
	// the left edge are skipped up front.
	hints := m.lineInlayHints(line)
	hi := 0
	for hi < len(hints) && hints[hi].Col < left {
		hi++
	}
	hintStyle := lipgloss.NewStyle().Foreground(m.theme().InlayHint).Italic(true)
	emitHint := func(b *strings.Builder, disp int, h ilsp.InlayHint) int {
		text := hintText(h)
		if w := width - disp; lipgloss.Width(text) > w {
			text = truncate(text, w)
		}
		b.WriteString(hintStyle.Render(text))
		return disp + lipgloss.Width(text)
	}

	// View-option overlays (#64), precomputed per span: the first column of
	// the trailing-whitespace run, the end of the leading indent, and the
	// display-cell offset of `from` measured from the line start so indent
	// guides and rulers align with the line's own columns regardless of
	// horizontal scroll or wrap segment.
	trailStart := len(runes)
	if m.wsMode != wsNone {
		for trailStart > 0 && (runes[trailStart-1] == ' ' || runes[trailStart-1] == '\t') {
			trailStart--
		}
	}
	indentEnd := 0
	if m.indentGuides {
		for indentEnd < len(runes) && (runes[indentEnd] == ' ' || runes[indentEnd] == '\t') {
			indentEnd++
		}
	}
	wsStyle := lipgloss.NewStyle().Foreground(m.theme().Whitespace)
	guideStyle := lipgloss.NewStyle().Foreground(m.theme().IndentGuide)
	rulerBG := m.theme().Ruler
	isRuler := func(cell int) bool {
		for _, r := range m.rulers {
			if r == cell {
				return true
			}
		}
		return false
	}
	startCells := 0
	for c := 0; c < from && c < len(runes); c++ {
		if runes[c] == '\t' {
			startCells += m.tabWidth
		} else {
			startCells++
		}
	}

	var b strings.Builder
	disp := 0         // display cells emitted so far (buffer cells + inlay hints)
	contentCells := 0 // buffer cells emitted so far (excludes inlay hints)
	for col := left; disp < width && (to < 0 || col < to); col++ {
		for hi < len(hints) && hints[hi].Col == col && disp < width {
			disp = emitHint(&b, disp, hints[hi])
			hi++
		}
		if disp >= width {
			break
		}
		cursorHere := isCursorLine && col == m.cursor.Col
		caretHere := m.focused && m.caretOnLine(line, col)
		selected := hasSel && col >= selStart && col <= selEnd
		if col >= len(runes) && !cursorHere && !caretHere && !selected {
			// Nothing meaningful left on this line; flush hints anchored at or
			// past the line end (a type hint after the last token) first.
			for hi < len(hints) && disp < width {
				disp = emitHint(&b, disp, hints[hi])
				hi++
			}
			break
		}

		cell, cells := " ", 1
		var overlay *lipgloss.Style // whitespace/guide foreground for this cell (#64)
		abs := startCells + contentCells
		if col < len(runes) {
			switch r := runes[col]; {
			case r == '\t':
				cell, cells = strings.Repeat(" ", m.tabWidth), m.tabWidth
				if m.wsVisible(col, trailStart) {
					cell = "→" + strings.Repeat(" ", cells-1)
					overlay = &wsStyle
				} else if m.guideAt(col, indentEnd, abs) {
					cell = "│" + strings.Repeat(" ", cells-1)
					overlay = &guideStyle
				}
			case r == ' ' && m.wsVisible(col, trailStart):
				cell = "·"
				overlay = &wsStyle
			case r == ' ' && m.guideAt(col, indentEnd, abs):
				cell = "│"
				overlay = &guideStyle
			default:
				cell = string(r)
			}
		}
		if disp+cells > width { // clamp a tab straddling the right edge
			cells = width - disp
			cell = strings.Repeat(" ", cells)
			overlay = nil
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
			// Cursor on a tab: highlight only the first cell (which may carry
			// a whitespace/guide glyph), leave the rest plain.
			b.WriteString(cursorStyle.Render(string([]rune(cell)[0])))
			b.WriteString(strings.Repeat(" ", cells-1))
		case cursorHere:
			b.WriteString(cursorStyle.Render(cell))
		case caretHere && cells > 1:
			b.WriteString(caretStyle.Render(string([]rune(cell)[0])))
			b.WriteString(strings.Repeat(" ", cells-1))
		case caretHere:
			b.WriteString(caretStyle.Render(cell))
		case selected:
			b.WriteString(selStyle.Render(cell))
		case inCurrent:
			b.WriteString(curMatchStyle.Render(cell))
		case inMatch:
			b.WriteString(matchStyle.Render(cell))
		default:
			st, styled := m.styleAt(line, col)
			if overlay != nil {
				// Whitespace glyphs / indent guides (#64) replace the syntax
				// colour; cursor/selection/search already won above.
				st, styled = *overlay, true
			}
			if isRuler(abs) {
				// Ruler tint (#64): a background stripe under everything the
				// higher-priority overlays didn't claim.
				st = st.Background(rulerBG)
				styled = true
			}
			if kind, ok := m.occurrenceAt(line, col); ok {
				// Occurrence mark (#172): a subtle background under the syntax
				// colour; cursor/selection/search already won above.
				st = st.Background(m.occurrenceColor(kind))
				styled = true
			}
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
		contentCells += cells
	}
	// Ruler columns past the content end (#64): pad with blanks so the ruler
	// reads as a continuous stripe on short lines. Only the unbounded final
	// span pads — wrapped middle segments are already full width.
	if to < 0 && len(m.rulers) > 0 {
		maxRuler := 0
		for _, r := range m.rulers {
			if r > maxRuler {
				maxRuler = r
			}
		}
		rulerStyle := lipgloss.NewStyle().Background(rulerBG)
		for abs := startCells + contentCells; disp < width && abs <= maxRuler; abs++ {
			if isRuler(abs) {
				b.WriteString(rulerStyle.Render(" "))
			} else {
				b.WriteString(" ")
			}
			disp++
		}
	}
	return b.String()
}

// wsVisible reports whether the whitespace rune at col renders as a visible
// glyph (#64) given the first column of the line's trailing-whitespace run.
func (m Model) wsVisible(col, trailStart int) bool {
	switch m.wsMode {
	case wsAll:
		return true
	case wsTrailing:
		return col >= trailStart
	}
	return false
}

// guideAt reports whether the whitespace cell at col (display cell abs from
// the line start) carries an indent guide (#64): inside the leading indent, on
// a tab-stop column past the first.
func (m Model) guideAt(col, indentEnd, abs int) bool {
	return m.indentGuides && col < indentEnd && abs > 0 && abs%m.tabWidth == 0
}

// DisplayOffset converts a buffer column on a line to its display-cell offset
// from the left edge of the text area, accounting for horizontal scroll, tab
// expansion, and injected inlay hints (#171) — so overlays anchored at a
// buffer cell align with what renderLine actually drew.
func (m Model) DisplayOffset(line, col int) int {
	runes := []rune(m.buf.Line(line))
	from := m.view.Left
	if m.softWrap {
		// Under soft wrap (#64) the offset counts from the cell's own wrap
		// segment; DisplayRow supplies the matching row.
		segs := m.wrapSegs(line)
		from = segs[viewport.SegmentIndex(segs, col)]
	}
	disp := 0
	for c := from; c < col; c++ {
		if c < len(runes) && runes[c] == '\t' {
			disp += m.tabWidth
		} else {
			disp++
		}
	}
	for _, h := range m.lineInlayHints(line) {
		// A hint anchored exactly at col renders before that cell, so it
		// shifts the cell too.
		if h.Col >= from && h.Col <= col {
			disp += lipgloss.Width(hintText(h))
		}
	}
	return disp
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

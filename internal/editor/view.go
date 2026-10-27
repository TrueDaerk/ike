package editor

import (
	"image/color"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
	"ike/internal/editor/search"
	"ike/internal/editor/textobject"
	"ike/internal/editor/viewport"
	"ike/internal/hscroll"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/theme"
	"ike/internal/ui"
	"ike/internal/unihint"
	"ike/internal/vcs"
)

// ModeColor is the accent colour that identifies an input mode (#1323), shared
// by the caret cell and the status line's mode badge so both signals agree.
// The colours come from existing palette slots, so every theme — built-in or
// third-party — carries them without a schema change.
func ModeColor(md Mode, pal *theme.Palette) color.Color {
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	switch {
	case md == Insert:
		return pal.Success
	case md.IsVisual():
		return pal.Warning
	case md == Replace:
		return pal.Error
	case md == Command:
		return pal.Info
	default:
		return pal.Accent
	}
}

// insertCaretTintFrac is how much of the mode colour mixes into the surface
// behind the insert-mode underline caret: enough to locate the caret at a
// glance, light enough that the character under it stays readable (the caret
// no longer inverts the cell, so the glyph keeps the theme's own contrast).
const insertCaretTintFrac = 0.15

// cursorStyle is the primary caret cell's style. The caret carries the input
// mode in two ways (#1353): its colour (#1323) and — the signal that reads
// without comparing colours — its shape. Normal, visual, command and replace
// mode draw the vim-conventional solid block; insert mode draws an underline
// caret over a light tint, so "typing inserts text" is obvious at the caret
// itself instead of only in the status line.
func (m Model) cursorStyle() lipgloss.Style {
	pal := m.theme()
	c := ModeColor(m.mode, pal)
	if m.mode == Insert {
		bg := theme.Mix(c, pal.Background, insertCaretTintFrac)
		return lipgloss.NewStyle().
			Background(bg).
			Foreground(theme.Readable(bg, pal.Background, pal.Foreground)).
			UnderlineStyle(lipgloss.UnderlineDouble).
			UnderlineColor(c).
			Bold(true)
	}
	return lipgloss.NewStyle().
		Background(c).
		Foreground(theme.Readable(c, pal.Background, pal.Foreground))
}

// conflictTintFrac is how much of the VCS status colour mixes into the surface
// for the ours/theirs section backgrounds (#1149) — subtle, like the diff
// pane's slotOrMix fractions.
const conflictTintFrac = 0.22

// doubleClickWindow is the maximum delay between two clicks on the same cell
// for them to escalate the click streak (matching the explorer's window).
const doubleClickWindow = 400 * time.Millisecond

// MouseClick moves the cursor to the content-local cell (x, y) — coordinates
// relative to the editor's content area (gutter included, title/border excluded).
// It maps the click through the gutter width and the current scroll offsets. In
// insert/replace mode the cursor may land one past the line end; otherwise it
// snaps onto a character.
//
// Consecutive clicks on the same cell within doubleClickWindow escalate
// click → word → line (#975): a double-click selects the word under the
// pointer, a triple-click the whole line — both as regular visual selections,
// so the usual operators and cmd+c/x apply. A later plain click collapses a
// mouse-made selection — and a Shift+arrow one (#2608) — back to a bare cursor;
// Shift+click extends instead (see ShiftClick).
func (m *Model) MouseClick(x, y int) {
	p := m.clickPosition(x, y)
	// A plain click returns to single-caret editing (#145).
	m.collapseCarets()
	switch m.bumpClickStreak(p) {
	case 2:
		if m.selectWordAt(p) {
			m.scroll()
			m.emit(EventCursorMove)
			return
		}
	case 3:
		m.selectLineAt(p)
		m.scroll()
		m.emit(EventCursorMove)
		return
	}
	if m.clickVisual {
		// The previous selection came from a multi-click: a plain click
		// collapses it (JetBrains behavior). v/V selections keep their
		// click-extends semantics.
		m.mode = Normal
		m.clickVisual = false
	} else if m.shiftSelect {
		// A selection started with Shift+arrows is GUI-style (#326): a plain
		// click drops it and places the caret, exactly like an unshifted
		// navigation key does (#2608).
		m.dropShiftSelection()
	}
	m.cursor = p
	m.desiredCol = m.cursor.Col
	m.scroll()
	m.emit(EventCursorMove)
}

// ShiftClick extends the active selection to the content-local cell (x, y)
// (#2608): the GUI counterpart of Shift+arrows, mirroring the explorer's
// shift+click. Without a selection it starts one at the current cursor — a
// shift-select selection, so an unshifted key or a plain click drops it again.
// It never advances the multi-click streak, so a following double-click still
// starts a fresh streak.
func (m *Model) ShiftClick(x, y int) {
	p := m.clickPosition(x, y)
	m.collapseCarets()
	m.clickStreak = 0
	if !m.mode.IsVisual() {
		m.enterVisual(Visual)
		m.shiftSelect = true
	}
	m.cursor = m.buf.ClampCursor(p)
	m.desiredCol = m.cursor.Col
	m.scroll()
	m.emit(EventCursorMove)
}

// ContextClick positions the caret for a right-click (#1020), JetBrains-style:
// a click inside the current selection keeps it (the context menu then acts on
// the selection), anything else moves the caret to the clicked cell, collapsing
// any selection and secondary carets. It never advances the multi-click streak.
func (m *Model) ContextClick(x, y int) {
	p := m.clickPosition(x, y)
	if m.selectionContains(p) {
		return
	}
	m.collapseCarets()
	if m.mode.IsVisual() {
		m.mode = Normal
		m.clickVisual = false
	}
	m.cursor = m.buf.ClampCursor(p)
	m.desiredCol = m.cursor.Col
	m.scroll()
	m.emit(EventCursorMove)
}

// selectionContains reports whether p lies inside the active visual selection
// (line-wise for VisualLine, inclusive char-wise otherwise; block falls back
// to charwise, matching visualSelection).
func (m *Model) selectionContains(p buffer.Position) bool {
	if !m.mode.IsVisual() {
		return false
	}
	a, b := m.anchor, m.cursor
	if a.Line > b.Line || (a.Line == b.Line && a.Col > b.Col) {
		a, b = b, a
	}
	if p.Line < a.Line || p.Line > b.Line {
		return false
	}
	if m.mode == VisualLine {
		return true
	}
	if p.Line == a.Line && p.Col < a.Col {
		return false
	}
	if p.Line == b.Line && p.Col > b.Col {
		return false
	}
	return true
}

// bumpClickStreak advances the multi-click counter for a click at p and
// returns the streak (1 = single, 2 = double, 3 = triple). A different cell
// or a pause beyond doubleClickWindow restarts at 1; a fourth click cycles
// back to a plain click.
func (m *Model) bumpClickStreak(p buffer.Position) int {
	now := time.Now()
	if m.clickNow != nil {
		now = m.clickNow()
	}
	if p == m.lastClickPos && m.clickStreak > 0 && now.Sub(m.lastClickAt) <= doubleClickWindow {
		m.clickStreak = m.clickStreak%3 + 1
	} else {
		m.clickStreak = 1
	}
	m.lastClickAt, m.lastClickPos = now, p
	return m.clickStreak
}

// selectWordAt selects the word under p as a charwise visual selection
// (double-click, #975), using the vim "iw" word classes. Reports false when
// the line is empty, in which case the click falls back to a plain click.
func (m *Model) selectWordAt(p buffer.Position) bool {
	res := textobject.Word(m.buf, p, false, false)
	if !res.OK {
		return false
	}
	m.cursor = res.Range.Start
	m.enterVisual(Visual)
	end := res.Range.End
	if end.Col > 0 {
		end.Col-- // End is exclusive; put the cursor on the last selected rune
	}
	m.cursor = m.buf.ClampCursor(end)
	m.desiredCol = m.cursor.Col
	m.clickVisual = true
	m.dragWord = res.Range
	return true
}

// MouseDrag extends a selection while the left button is held (#977): the
// press cell anchors, and each motion event grows the selection to the cell
// under the pointer — char-wise for a plain press, word-wise after a
// double-click (the origin word stays fully selected), line-wise after a
// triple-click. A drag while a keyboard-entered (v/V) selection is active
// keeps extending that selection instead of re-anchoring.
func (m *Model) MouseDrag(x, y int) {
	p := m.clickPosition(x, y)
	switch {
	case m.clickStreak == 2 && m.mode.IsVisual():
		m.dragWordTo(p)
	case m.clickStreak == 3 && m.mode.IsVisual():
		m.cursor = m.buf.ClampCursor(p)
		m.desiredCol = m.cursor.Col
	default:
		m.dragCharTo(p)
	}
	m.scroll()
	m.emit(EventCursorMove)
}

// dragCharTo extends a char-wise drag selection to p, entering visual mode on
// the first cell of travel. Inside an existing selection it only moves the
// cursor (the drag then extends it, keyboard- and mouse-made alike).
func (m *Model) dragCharTo(p buffer.Position) {
	if !m.mode.IsVisual() {
		if p == m.lastClickPos {
			return // no travel yet: still a plain click
		}
		m.cursor = m.lastClickPos
		m.enterVisual(Visual)
		m.clickVisual = true
	}
	m.cursor = m.buf.ClampCursor(p)
	m.desiredCol = m.cursor.Col
}

// dragWordTo extends a word-wise drag (after a double-click) to the word
// under p. The origin word stays fully selected: dragging forward anchors at
// its first rune, dragging backward at its last.
func (m *Model) dragWordTo(p buffer.Position) {
	res := textobject.Word(m.buf, p, false, false)
	if !res.OK {
		return
	}
	last := func(r buffer.Range) buffer.Position {
		e := r.End
		if e.Col > 0 {
			e.Col-- // End is exclusive; the last selected rune
		}
		return e
	}
	if res.Range.Start.Before(m.dragWord.Start) {
		m.anchor = m.buf.ClampCursor(last(m.dragWord))
		m.cursor = res.Range.Start
	} else {
		m.anchor = m.dragWord.Start
		m.cursor = m.buf.ClampCursor(last(res.Range))
	}
	m.desiredCol = m.cursor.Col
}

// selectLineAt selects the whole clicked line as a linewise visual selection
// (triple-click, #975), cursor on the line's last rune.
func (m *Model) selectLineAt(p buffer.Position) {
	m.cursor = m.buf.ClampCursor(buffer.Position{Line: p.Line})
	m.enterVisual(VisualLine)
	end := len([]rune(m.buf.Line(p.Line))) - 1
	if end < 0 {
		end = 0
	}
	m.cursor = m.buf.ClampCursor(buffer.Position{Line: p.Line, Col: end})
	m.desiredCol = m.cursor.Col
	m.clickVisual = true
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
	m.dismissPeek()
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
	off := x - m.view.GutterWidth(m.buf.LineCount())
	if off < 0 {
		off = 0 // a gutter click snaps to the row's first visible column
	}
	p := buffer.Position{Line: line, Col: m.displayClickCol(line, colBase, off)}
	if m.mode == Insert || m.mode == Replace {
		return m.buf.Clamp(p)
	}
	return m.buf.ClampCursor(p)
}

// displayClickCol is the mouse map's inverse for a row's display cells: the
// clicked offset counts cells from buffer column from, where a tab occupies
// tabWidth cells (View expands it), concealed columns emit nothing (#881),
// and a stand-in range renders its replacement's cells at the range start
// (#1585) — so the buffer column is the one whose display prefix covers the
// offset. A click inside a tab's cells lands on the tab itself, one inside a
// stand-in lands on the range start; offsets past the line end map 1:1
// (everything there is one-cell padding).
func (m Model) displayClickCol(line, from, offset int) int {
	// A box-drawn table row (#881) maps through the cell layout instead
	// (#1599): the click lands in the pointed-at cell, borders on their pipe.
	if col, ok := m.tableClickCol(line, from+offset); ok {
		return col
	}
	if m.svActive() {
		// sv horizontal scroll is display-space (#1724): from counts cells of
		// the aligned row, so fold it in and walk from the line start.
		offset += from
		from = 0
	}
	runes := []rune(m.buf.Line(line))
	conceals := m.lineConcealRanges(line)
	cw := m.lineCells(runes)
	col := from
	for ; col < len(runes); col++ {
		if cr, ok := rangeAt(conceals, col); ok {
			if cr.repl != "" && col == cr.start {
				if cells := lipgloss.Width(cr.repl); offset < cells {
					// Map the click proportionally into the raw value (#1782):
					// a stand-in generally doesn't correspond rune-for-rune to
					// the source it replaces, so scale the offset by the ratio
					// of raw length to replacement width instead of always
					// landing on the range start.
					last := cr.end - 1
					mapped := col
					switch {
					case cells <= 1:
						// One display cell to click on: it stands for the
						// whole range, so keep the range-start behaviour.
						mapped = col
					case offset == cells-1:
						// The last cell always lands exactly on the last raw
						// column — the round-based scale below only
						// approaches it, so pin it explicitly.
						mapped = last
					default:
						rangeLen := cr.end - cr.start
						mapped = col + (offset*rangeLen+cells/2)/cells
						if mapped > last {
							mapped = last
						}
					}
					if mapped < col {
						mapped = col
					}
					return mapped
				} else {
					offset -= cells
				}
			}
			continue
		}
		// A cluster's cells all belong to its head column (#2526): the head
		// consumes the whole width, the absorbed columns consume nothing, so
		// a click inside a wide glyph lands on the glyph's first rune.
		cells := cw.at(runes, col, m.tabWidth)
		if offset < cells {
			return col
		}
		offset -= cells
	}
	return col + offset
}

// ScrollBy moves the viewport by delta lines (positive down, negative up)
// without moving the cursor, clamped to the buffer — a mouse-wheel scroll,
// independent of mode. Vertical only; see ScrollXBy for horizontal.
func (m *Model) ScrollBy(delta int) {
	defer m.refreshFollowPause() // a wheel scroll pauses/resumes follow (#1928)
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
	sv := m.svActive()
	for i := m.view.Top; i < m.view.Bottom(m.buf.LineCount()); i++ {
		n := len([]rune(m.buf.Line(i)))
		if sv {
			// sv rows scroll in display space (#1724): the reachable width is
			// the aligned row's expansion, not the raw rune count.
			n = m.svDisplayCol(i, n)
		}
		if n > maxLen {
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
	if left != m.view.Left {
		m.bumpRender() // horizontal scroll shifts every line's column window (#614)
	}
	m.view.Left = left
}

// CommandLine returns the text shown on the command line: ":cmd" in ex mode,
// "/pat" / "?pat" while searching, or the follow filter's "|pat" / "*pat"
// (#2255). It is "" outside command mode.
func (m Model) CommandLine() string {
	if m.mode != Command {
		return ""
	}
	return m.cmdPrefix() + m.cmdline
}

// cmdPrefix is the one-character kind marker the command line opens with.
func (m Model) cmdPrefix() string {
	switch {
	case m.filtering && m.logFilt.hilite:
		return "*" // follow highlight (#2255)
	case m.filtering:
		return "|" // follow filter, grep-style
	case m.searching && m.searchDir == 0: // search.Forward
		return "/"
	case m.searching:
		return "?"
	}
	return ":"
}

// View renders the buffer: a line-number gutter (when enabled), horizontally and
// vertically scrolled text, and the cursor cell highlighted when focused.
func (m Model) View() string {
	m.syncEpoch() // drop stale line-cache entries if the render epoch moved (#614)
	if m.path == "" && m.buf.LineCount() == 1 && m.buf.Line(0) == "" {
		// The command line must show even on the empty scratch buffer (":q").
		if cl := m.commandLineRow(); cl != "" {
			return cl
		}
		return lipgloss.NewStyle().Faint(true).Render("(no file open)")
	}
	lineCount := m.buf.LineCount()
	gutterStyle := lipgloss.NewStyle().Faint(true)
	cursorStyle := m.cursorStyle()
	textWidth := m.view.TextWidth(lineCount)
	// Right-aligned annotations (blame, log delta hints) must stop one column
	// short of the scrollbar: overlayScrollbar claims the pane's rightmost
	// column, so an annotation padded to the full text width loses its last
	// cell — "+460ms" reads as "+460m" (#1728).
	annotWidth := m.annotColumnWidth()

	selStyle := lipgloss.NewStyle().Background(m.theme().SelectionMuted)

	var out []string
	// Sticky scroll (#168): the enclosing declaration headers replace the top
	// rows of the pane; the buffer lines they cover are skipped, so the first
	// content row is the line right below the innermost pinned scope header.
	sticky := m.stickyLines()
	for si, line := range sticky {
		gutter := gutterStyle.Render(m.view.Gutter(line, m.cursor.Line, lineCount))
		body, over := m.renderLine(line, textWidth, cursorStyle, selStyle)
		// The pinned headers scroll with the buffer, so they carry the same
		// edge marks (#2377) — applied before the separator, which styles the
		// row as a whole.
		body = m.stampHScroll(body, line, textWidth, over)
		if si == len(sticky)-1 {
			// The last pinned row carries the subtle separator (#1910).
			body = m.stickySeparate(body, textWidth)
		}
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
	bps := m.breakpointSet()
	bpsOff := m.disabledBreakpointSet()
	bpsCond := m.conditionalBreakpointSet()
	bpsLog := m.logpointSet()
	tests := m.testMarks()
	bookmarks := m.bookmarkSigns()
	for i := m.view.Top + len(sticky); len(out) < height && i < lineCount; i++ {
		if m.lineHidden(i) {
			continue
		}
		gs := gutterStyle
		// Colour the gutter for a line carrying diagnostics (red error / yellow warn),
		// the cheap sign-column indicator that keeps the gutter width unchanged.
		// A git diff marker (Roadmap 0320, #464) colours the same way; a
		// diagnostic wins the cell when both apply. A breakpoint (0350, #577)
		// and the debugger's current line (#579) outrank both and, unlike the
		// colour-only markers, draw a distinct glyph in the separator cell so
		// they never read as a git highlight: ● breakpoint, ▶ paused line.
		// A test run marker (#1150) — ▶ in the success tone on a test
		// declaration line — slots between the breakpoint glyph and the
		// colour-only markers: a breakpoint on a test line keeps its ●
		// (breakpoints stay toggleable everywhere), while the run marker
		// still outranks diagnostic/git colouring of the sign cell.
		var sign string
		var signStyle lipgloss.Style
		if m.paused && i == m.pausedLine {
			sign = "▶"
			signStyle = lipgloss.NewStyle().Foreground(m.theme().Warning).Bold(true)
		} else if bps[i] {
			// The breakpoint kind picks the glyph (#2245): ● plain, ◉
			// conditional (condition and/or hit count), ◆ logpoint in the
			// warning tone — it logs instead of stopping. A disabled
			// breakpoint (#1377) keeps its slot and its shape but draws
			// hollow and faint — visible, not armed.
			sign = "●"
			tone := m.theme().Error
			switch {
			case bpsLog[i]:
				sign, tone = "◆", m.theme().Warning
				if bpsOff[i] {
					sign = "◇"
				}
			case bpsCond[i]:
				sign = "◉"
				if bpsOff[i] {
					sign = "◎"
				}
			case bpsOff[i]:
				sign = "○"
			}
			signStyle = lipgloss.NewStyle().Foreground(tone).Bold(true)
			if bpsOff[i] {
				signStyle = lipgloss.NewStyle().Foreground(tone).Faint(true)
			}
		} else if glyph, ok := bookmarks[i]; ok {
			// A bookmark/mark glyph (#1151) slots below the breakpoint —
			// breakpoints stay visible everywhere — and above the test run
			// marker: a vim mark draws its own letter (#2661, lowercase local
			// / uppercase global — with several marks in one file the glyph
			// alone said nothing), a project bookmark (#55) its mnemonic digit
			// or the anonymous ⚑. bookmarkSigns resolves the precedence.
			sign = glyph
			signStyle = lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true)
		} else if _, ok := tests[i]; ok {
			sign = "▶"
			signStyle = lipgloss.NewStyle().Foreground(m.theme().Success).Bold(true)
		} else if kind, ok := m.inheritMarkAt(i); ok {
			// Inheritance arrows (#1453) are informational glyphs: below every
			// actionable marker, above the colour-only diagnostic/git tints.
			sign = inheritSign(kind)
			signStyle = lipgloss.NewStyle().Foreground(m.theme().Info)
		} else if ck, stale, ok := m.coverMarkAt(i); ok {
			// Coverage marks (#2081): a JetBrains-style covered/uncovered bar
			// in the sign column — green covered, red uncovered, yellow
			// partial — below the informational glyphs, above the colour-only
			// tints. Stale data (the buffer changed since the run) draws
			// faint in the neutral info tone so it never reads as current.
			sign = "▎"
			switch {
			case stale:
				signStyle = lipgloss.NewStyle().Foreground(m.theme().Info).Faint(true)
			case ck == lang.CoverUncovered:
				signStyle = lipgloss.NewStyle().Foreground(m.theme().Error)
			case ck == lang.CoverPartial:
				signStyle = lipgloss.NewStyle().Foreground(m.theme().Warning)
			default:
				signStyle = lipgloss.NewStyle().Foreground(m.theme().Success)
			}
		} else if g, ok := m.hostSigns[i]; ok {
			// Host glyphs (#2789): informational, below every built-in marker.
			// A host change mark on the same line (#2787) recolours the glyph
			// rather than replacing it, so the value boundary stays visible.
			sign = g
			signStyle = lipgloss.NewStyle().Foreground(m.theme().Hint)
			if mk, ok := m.hostChanges[i]; ok {
				signStyle = lipgloss.NewStyle().Foreground(m.theme().Info).Bold(true)
				if mk != vcs.LineDeleted {
					gs = lipgloss.NewStyle().Foreground(m.theme().Info)
				}
			}
		} else if mk, ok := m.hostChanges[i]; ok {
			// Host change marks (#2787): the playground's "changed since the
			// previous run" bars, in the Info tone.
			sign = hostChangeSign(mk)
			signStyle = lipgloss.NewStyle().Foreground(m.theme().Info)
			if mk != vcs.LineDeleted {
				gs = lipgloss.NewStyle().Foreground(m.theme().Info)
			}
		} else if sev, ok := m.worstSeverityOnLine(i); ok {
			gs = lipgloss.NewStyle().Foreground(m.diagColor(sev))
		} else if mk, ok := m.gitMarks[i]; ok && m.gitVisible(mk) {
			gs = lipgloss.NewStyle().Foreground(m.gitMarkColor(mk))
		}
		raw := m.view.Gutter(i, m.cursor.Line, lineCount)
		var gutter string
		if sign != "" && raw != "" {
			// Overwrite the leading sign column with the glyph so the marker
			// sits before the line number; the gutter width stays constant so
			// the body never reflows.
			gutter = signStyle.Render(sign) + gs.Render(raw[1:])
		} else {
			gutter = gs.Render(raw)
		}
		// The collapsed-run headers below each end in a right-aligned tag of
		// their own (fold placeholder, ×N badge, PEM summary); that tag *is*
		// the row's right edge, so they take the left mark only (#2377).
		if end, ok := m.foldedAt(i); ok {
			body := m.renderFoldHeader(i, end, textWidth, cursorStyle, selStyle)
			out = append(out, gutter+m.stampHScroll(body, i, textWidth, false))
			continue
		}
		// A run of identical log lines (#1650) collapses the same way, into
		// its first line plus a ×N badge in the annotation column (#1734).
		if end, ok := m.logRunAt(i); ok {
			body := m.renderLogRunHeader(i, end, textWidth, annotWidth, cursorStyle, selStyle)
			out = append(out, gutter+m.stampHScroll(body, i, textWidth, false))
			continue
		}
		// A PEM block (#1652) collapses onto its BEGIN marker plus the decoded
		// summary — CN, validity, issuer, key type — instead of a base64 wall.
		if b, ok := m.pemBlockAt(i); ok {
			body := m.renderPemHeader(i, b, textWidth, cursorStyle, selStyle)
			out = append(out, gutter+m.stampHScroll(body, i, textWidth, false))
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
				// No marks under soft wrap: there is no horizontal scroll and
				// no segment runs past the edge (#2377, stampHScroll).
				seg, _ := m.renderSpan(i, segs[si], to, textWidth, cursorStyle, selStyle)
				out = append(out, g+seg)
			}
			continue
		}
		row, over := m.renderLine(i, textWidth, cursorStyle, selStyle)
		row = m.stampHScroll(row, i, textWidth, over)
		if spanned, ok := m.logSpanAnnotate(row, i, annotWidth); ok {
			// The span delta of a log selection (#1736) outranks both: it
			// answers a question the user just asked, on the row they move.
			row = spanned
		} else if dbg, ok := m.debugValueAnnotate(row, i, annotWidth); ok {
			// Inline debugger values (#1914): the stopped frame's locals are
			// the state the user is inspecting right now.
			row = dbg
		} else if flight, ok := m.httpFlightAnnotate(row, i, annotWidth); ok {
			// A request dispatched from this file is still running (#1746):
			// live state outranks blame, which is not going anywhere.
			row = flight
		} else if m.blameOn && i == m.cursor.Line {
			// Inline blame (0320, #468): the annotation splices into the
			// cursor line's right padding when it fits.
			row = m.blameAnnotate(row, i, annotWidth)
		} else {
			// A log line's elapsed-time hint (#1651) uses the same padding,
			// so blame keeps the cursor line to itself.
			row = m.logDeltaAnnotate(row, i, annotWidth)
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
	// Vertical scrollbar with diagnostics error stripe (#1022): overlays the
	// rightmost column whenever the buffer overflows the viewport.
	out = m.overlayScrollbar(out)
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
	// cl is the prefix rune (":", "/" or "?") plus the typed line; render the
	// line through the shared cursor view so the movable cursor (#1110) shows.
	line := ui.CursorView(m.cmdline, m.cmdCur)
	if m.cmdSelStart < m.cmdSelEnd {
		selStyle := lipgloss.NewStyle().Background(m.theme().Selection).Foreground(m.theme().SelectionText)
		line = ui.CursorViewSel(m.cmdline, m.cmdCur, m.cmdSelStart, m.cmdSelEnd, selStyle)
	}
	row := m.cmdPrefix() + line + m.searchCounter() + m.filterHint() + m.suggestRow()
	return row + m.searchHistoryHint(row)
}

// filterHint trails the open follow-filter line (#2255) with what the pattern
// does right now: the number of lines it keeps, or the compile error of a
// regex that is not valid (yet) — the inline error the filter reports instead
// of silently matching a literal.
func (m Model) filterHint() string {
	if !m.filtering {
		return ""
	}
	f := m.logFilt
	switch {
	case f.err != "":
		return lipgloss.NewStyle().Foreground(m.theme().Error).Render("  " + f.err)
	case !f.active():
		return ""
	}
	hint := "no matches"
	if n := m.logFilterCount(); n > 0 {
		hint = strconv.Itoa(n) + " lines"
		if f.hilite {
			hint += " highlighted"
		}
	}
	return lipgloss.NewStyle().Faint(true).Render("  " + hint)
}

// suggestRow renders the path-completion hint after the ":"-line cursor
// (#543): the ambiguous candidates' names, dimmed. Empty while searching or
// without an ambiguous completion.
func (m Model) suggestRow() string {
	if m.searching {
		return ""
	}
	hint := m.suggestHint()
	if hint == "" {
		return ""
	}
	return lipgloss.NewStyle().Faint(true).Render("  " + hint)
}

// searchCounter renders the incremental-search tally for the command-line row
// (#255): current match index over total ("999+" past the scan cap, #2145), or
// "no matches" for a pattern that hits nothing. Empty outside an active search
// line or on an empty pattern; the status line keeps showing the same counter
// after the line closes (SearchCounter, searchtally.go).
func (m Model) searchCounter() string {
	if !m.searching {
		return ""
	}
	// Structural mode (#2363) trails its own state: the evaluation error in
	// error colour — the inline report instead of zero silent matches — or a
	// faint mode label while the query line is still empty.
	if err := m.preview.StructuralErr(); err != "" {
		return lipgloss.NewStyle().Foreground(m.theme().Error).Render("  " + err)
	}
	if m.preview.IsStructural() && m.preview.Empty() {
		return lipgloss.NewStyle().Faint(true).Render("  structural (jq)")
	}
	if m.preview.Empty() {
		return ""
	}
	return lipgloss.NewStyle().Faint(true).Render("  " + m.SearchCounter())
}

// searchHLQuery returns the query whose matches the view highlights: the live
// preview while the search line or the replace panel (#283) is open, else the
// committed query while highlights are armed (until a normal-mode Esc clears
// them, #255).
func (m Model) searchHLQuery() (search.Query, bool) {
	if (m.searching || m.replPanel != nil) && !m.preview.Empty() {
		m.preview.SyncStructural(m.buf, m.docVersion)
		return m.preview, true
	}
	if m.hlActive && !m.query.Empty() {
		// A structural query (#2363) re-evaluates when edits moved the
		// document version; the shared state makes this visible to every
		// copy of the query.
		m.query.SyncStructural(m.buf, m.docVersion)
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
// It also reports whether the line ran past the window's right edge — the
// signal the horizontal-scroll right mark rides on (#2377).
func (m Model) renderLine(line, width int, cursorStyle, selStyle lipgloss.Style) (string, bool) {
	return m.renderSpan(line, m.view.Left, -1, width, cursorStyle, selStyle)
}

// renderSpan renders the columns [from, to) of one buffer line, memoized by the
// line cache (#614): within a render epoch the same span renders byte-identically,
// so a vertical scroll — which changes no epoch input — reuses the cached body
// instead of recomputing highlight, selection and width for every visible line.
func (m Model) renderSpan(line, from, to, width int, cursorStyle, selStyle lipgloss.Style) (string, bool) {
	key := lineKey{line: line, from: from, to: to, width: width}
	if sb, ok := m.cachedSpan(key); ok {
		return sb.body, sb.over
	}
	body, over := m.renderSpanUncached(line, from, to, width, cursorStyle, selStyle)
	m.storeSpan(key, spanBody{body: body, over: over})
	return body, over
}

// renderSpanUncached renders the columns [from, to) of one buffer line (to < 0
// means unbounded — through the content end). Under soft wrap (#64) each wrap
// segment is one span; the unwrapped renderLine is the single span starting at
// the horizontal scroll offset. Whitespace glyphs, indent guides, and ruler
// tints (#64) overlay here so both paths share them.
// It returns the rendered body plus whether the span still had content when
// the width budget ran out (#2377): the render is the only place that knows,
// since tabs, conceal stand-ins and inlay hints all make a line's display
// width something other than its rune count.
func (m Model) renderSpanUncached(line, from, to, width int, cursorStyle, selStyle lipgloss.Style) (string, bool) {
	// Markdown pipe tables (#881): while the cursor is outside the block the
	// whole row renders as its pre-built box-drawing form; the cell loop below
	// never runs. Entering the block flips it back to raw pipe source.
	if row, ok := m.mdTableRow(line); ok {
		_, over := hscroll.Cut(from, width, ansi.StringWidth(row))
		return m.renderTableRow(row, from, to, width), over
	}
	runes := []rune(m.buf.Line(line))
	left := from
	// Concealment (#881/#1585/#1589): cells inside a conceal range are
	// skipped (or replaced by their stand-in) so the line reads like
	// rendered text. lineConcealRanges already dropped the ranges the caret
	// sits in or a selection crosses — those cells render raw (#1594).
	conceals := m.lineConcealRanges(line)
	// Table-rendered sv buffers scroll in display space (#1724): view.Left
	// counts cells of the aligned (concealed) row, so slicing the expansion —
	// not the raw text — sheds the same width from every row and the shared
	// column edges survive the scroll. padSkip is the part of a stand-in
	// already off screen when the window edge lands inside its padding.
	padSkip := 0
	if from > 0 && m.svActive() {
		left, padSkip = m.svDisplaySlice(line, from)
	}
	// The decoration scans below cover the whole line while it is short and
	// only the rendered window with a margin once it is very long (#2734,
	// longline.go), so a minified file renders within the frame budget.
	dlo, dhi := decorWindow(len(runes), from, to, width)
	// Inline color preview (#790): literal cells tint with their own color.
	swatches := m.windowedColorSwatches(line, runes, dlo, dhi)
	// Identifier colors (#1626): UUIDs and hex hashes take a foreground from
	// the rainbow palette keyed on the identifier's own hash.
	ids := m.windowedIDColors(line, runes, dlo, dhi)
	// Terminal hyperlinks (#1655): the clickable ranges of this line — bare
	// URLs and Markdown link labels. Each of their cells wraps in its own
	// OSC 8 open/close pair below (see hyperlink.go).
	links := m.windowedLinks(runes, dlo, dhi)
	// Caret column stripe (#1659): the rune range this line contributes to the
	// table column the caret sits in, tinted below every other overlay.
	svStart, svEnd, hasSVCol := m.svColumnRange(line)
	svTint := m.svTintFor(hasSVCol)
	selStart, selEnd, hasSel := m.selectionOnLine(line, len(runes))
	isCursorLine := line == m.cursor.Line && m.focused
	// Secondary carets (#145) render dimmer than the primary cell.
	caretStyle := cursorStyle.Faint(true)

	// Search-match highlighting (#255): all matches of the active query get a
	// background; the span the cursor sits on (the current match) is also
	// underlined so it stands apart from the rest.
	matchSpans := m.lineSearchSpans(line, dlo, dhi)
	matchStyle := lipgloss.NewStyle().Background(m.theme().SelectionMuted)
	// The current match takes an accent-tinted background on top of the
	// underline (#2145) so it stays findable among the other highlights.
	curMatchStyle := lipgloss.NewStyle().
		Background(theme.Mix(m.theme().Accent, m.theme().Surface, 0.45)).
		Underline(true)

	// DOM inspector selector matches (#1929, dommatches.go) share the search
	// styles: every match gets the background, the pane's current match the
	// underline.
	domSpans := m.domLineSpans(line)

	// Follow-filter matches (#2255, logfilter.go): the highlight half of the
	// live filter. They take their own warning-tinted background rather than
	// the search styles — a filter and a search are two different questions
	// about the buffer, and the answers must stay tellable apart.
	filterSpans := m.logFilterSpans(line)
	filterStyle := lipgloss.NewStyle().
		Background(theme.Mix(m.theme().Warning, m.theme().Surface, 0.40))

	// Label-jump overlays (#787): a target's typed span highlights like a
	// search match, and its label characters draw over the buffer text above
	// every other decoration — a label must stay readable to be typable.
	leapActive := m.leap != nil && len(m.leap.query) > 0
	leapLabelStyle, leapMatchStyle := m.leapStyles(leapActive)

	// Inlay hints (#171): virtual text injected before the cell it anchors at,
	// dimmed and italic so it never reads as buffer content. Hints scrolled off
	// the left edge are skipped up front. Code lenses (#1912) ride the same
	// machinery as one synthetic trailing hint per line, emitted by the
	// end-of-line flush after the last buffer cell.
	hints := m.lineHintsWithLens(line)
	hi := firstHintFrom(hints, left)
	hintStyle := lipgloss.NewStyle().Foreground(m.theme().InlayHint).Italic(true)

	// View-option overlays (#64), precomputed per span: the first column of
	// the trailing-whitespace run, the end of the leading indent, and the
	// display-cell offset of `from` measured from the line start so indent
	// guides and rulers align with the line's own columns regardless of
	// horizontal scroll or wrap segment.
	trailStart := m.trailingWSStart(runes)
	indentEnd := m.leadingIndentEnd(runes)
	wsStyle := lipgloss.NewStyle().Foreground(m.theme().Whitespace)
	// Indent guides (#64) carry one style per depth slot when depth coloring
	// is on (#1628), a single flat style otherwise; guideSlot picks the entry.
	guideStyles := m.guideStyles()
	// Control bytes (#1469): render as one-cell ␛-style glyphs so nothing
	// reaches the terminal raw; SGR sequences additionally colour the text
	// they govern (see ansiescape.go).
	ctrlStyle := lipgloss.NewStyle().Foreground(m.theme().Whitespace)
	// Invisible/deceptive Unicode (#1654): zero-width characters, NBSP, the
	// soft hyphen and every bidi control render as a one-cell warn-coloured
	// placeholder — the editor never draws a character as nothing, and a
	// zero-width rune reaching the terminal raw outside a grapheme cluster
	// would desync the column-to-cell mapping like a raw control byte (#1469).
	invisStyle := lipgloss.NewStyle().Foreground(m.theme().Warning)
	sgrSpans := sgrSpansFor(runes)
	rulerBG := m.theme().Ruler
	// Cell layout (#2526, cells.go): the display width of every column —
	// wide glyphs take two cells, the columns a grapheme cluster absorbs take
	// none. The loop, the click map and DisplayOffset all read this table.
	cw := m.lineCells(runes)
	startCells := 0
	if left != from || padSkip > 0 {
		// sv display slice (#1724): from already is the display width shed.
		startCells = from
	} else {
		for c := 0; c < from && c < len(runes); c++ {
			startCells += cw.at(runes, c, m.tabWidth)
		}
	}

	glyphs := cellGlyphKit{
		ws: wsStyle, ctrl: ctrlStyle, invis: invisStyle, guides: guideStyles,
		trailStart: trailStart, indentEnd: indentEnd,
	}
	overlays := overlayStyles{
		cursor: cursorStyle, caret: caretStyle, leapLabel: leapLabelStyle,
		leapMatch: leapMatchStyle, sel: selStyle, curMatch: curMatchStyle,
		match: matchStyle, filter: filterStyle,
	}
	baseCtx := baseCellCtx{
		sgrSpans: sgrSpans, ctrl: ctrlStyle, hasSVCol: hasSVCol, svStart: svStart, svEnd: svEnd,
		svTint: svTint, rulerBG: rulerBG,
	}

	var b strings.Builder
	disp := 0         // display cells emitted so far (buffer cells + inlay hints)
	contentCells := 0 // buffer cells emitted so far (excludes inlay hints)
	col := left
	for ; disp < width && (to < 0 || col < to); col++ {
		for hi < len(hints) && hints[hi].Col == col && disp < width {
			disp = emitInlayHint(&b, hintStyle, width, disp, hints[hi])
			hi++
		}
		if disp >= width {
			break
		}
		if col < len(runes) && cw.at(runes, col, m.tabWidth) == 0 {
			// A column absorbed into the cluster before it (#2526): the head
			// column drew the whole glyph and carried every overlay for it.
			continue
		}
		// The cluster this column heads: [col, end). Cursor, carets and the
		// selection are tested against the whole range, since the caret may
		// sit on an absorbed column and a selection may start inside one.
		end := cw.clusterEnd(runes, col)
		cursorHere := isCursorLine && col <= m.cursor.Col && m.cursor.Col < end
		caretHere := m.focused && m.caretOnRange(line, col, end)
		selected := hasSel && end-1 >= selStart && col <= selEnd
		leapRune, leapHere, leapMatch := m.leapCellAt(leapActive, line, col)
		if col < len(runes) {
			if cr, ok := rangeAt(conceals, col); ok {
				// Concealed cell: marker chrome (#881) emits nothing; a
				// replacement range emits its stand-in once, at the range
				// start, styled by the range's own capture. A decoded
				// stand-in (#1585) additionally carries a background tint so
				// it reads as a stand-in, not buffer content — the sv
				// alignment padding (#1589) stays plain spacing. Display
				// columns close up either way; contentCells still advances
				// so ruler/guide positions keep tracking buffer cells.
				if cr.repl != "" && col == cr.start {
					repl := cr.repl
					if col == left && padSkip > 0 {
						// The window edge fell inside this stand-in (#1724):
						// only its tail is on screen.
						repl = string([]rune(repl)[padSkip:])
					}
					inSV := hasSVCol && col >= svStart && col < svEnd
					disp = m.writeStandIn(&b, line, col, width, disp, repl, cr.standIn, inSV, svTint)
				}
				contentCells += cw.at(runes, col, m.tabWidth)
				continue
			}
		}
		if col >= len(runes) && !cursorHere && !caretHere && !selected && !leapHere {
			// Nothing meaningful left on this line; flush hints anchored at or
			// past the line end (a type hint after the last token) first.
			disp, hi = m.flushLineEnd(&b, line, width, disp, hi, hints, hintStyle)
			break
		}

		cell, cells := " ", 1
		isTab := false
		var overlay *lipgloss.Style // whitespace/guide foreground for this cell (#64)
		abs := startCells + contentCells
		if col < len(runes) {
			cell, cells, isTab, overlay = m.cellGlyph(runes, col, end, abs, cw.at(runes, col, m.tabWidth), &glyphs)
		}
		if disp+cells > width { // clamp a tab or wide glyph straddling the right edge
			cells = width - disp
			cell = strings.Repeat(" ", cells)
			overlay = nil
		}

		inMatch, inCurrent := m.searchMatchAt(line, col, matchSpans)
		if !inMatch {
			for _, s := range domSpans {
				if col >= s.start && col < s.end {
					inMatch, inCurrent = true, s.cur
					break
				}
			}
		}
		inFilter := false
		if !inMatch {
			for _, s := range filterSpans {
				if col >= s.Start && col < s.End {
					inFilter = true
					break
				}
			}
		}

		// Terminal hyperlink (#1655): the cell's own OSC 8 open/close pair,
		// zero display cells wide, so width math and click mapping stay
		// untouched; the shared id keeps the terminal hover-highlighting the
		// whole URL as one link even though every cell closes its own pair
		// (a self-contained pair can never be split by a later row splice).
		var link linkSpan
		inLink := false
		if col < len(runes) {
			link, inLink = linkAt(links, col)
		}
		if inLink {
			b.WriteString(ansi.SetHyperlink(link.url,
				"id=ike-"+strconv.Itoa(line)+"-"+strconv.Itoa(link.start)))
		}

		if s, ok := renderOverlayCell(cellFlags{
			leapHere: leapHere, leapMatch: leapMatch, cursorHere: cursorHere,
			caretHere: caretHere, selected: selected, inCurrent: inCurrent,
			inMatch: inMatch, inFilter: inFilter, isTab: isTab,
			leapRune: leapRune, cell: cell, cells: cells,
		}, &overlays); ok {
			b.WriteString(s)
		} else {
			st, styled := m.baseCellStyle(line, col, abs, overlay, &baseCtx)
			st, styled = m.conflictCellStyle(line, st, styled)
			st, styled = m.markCellStyle(line, col, st, styled)
			for _, id := range ids {
				// Identifier colors (#1626): the hash-keyed rainbow color
				// replaces the syntax foreground, leaving the backgrounds
				// (ruler, conflict, occurrence) below it intact.
				if col >= id.Start && col < id.End {
					if idst, ok := m.idColorStyle(id); ok {
						st, styled = st.Foreground(idst.GetForeground()), true
					}
					break
				}
			}
			for _, sw := range swatches {
				// Color preview (#790): the literal's own color replaces the
				// syntax/ruler/occurrence styling; the diagnostic underline
				// still composes on top, cursor/selection/search already won.
				if col >= sw.Start && col < sw.End {
					st = swatchStyle(sw)
					styled = true
					break
				}
			}
			st, styled = m.finishCellStyle(line, col, st, styled)
			b.WriteString(renderStyledCell(st, styled, cell))
		}
		if inLink {
			b.WriteString(ansi.ResetHyperlink())
		}
		disp += cells
		contentCells += cells
	}
	// Ruler columns past the content end (#64): pad with blanks so the ruler
	// reads as a continuous stripe on short lines. Only the unbounded final
	// span pads — wrapped middle segments are already full width.
	disp = m.padRulerTail(&b, to, startCells+contentCells, disp, width, rulerBG)
	// The right-edge mark's condition (#2377): the width budget ran out with
	// buffer cells or inlay hints still queued. Read off the cell loop rather
	// than re-measured, so every width oddity it just handled — tabs, conceal
	// stand-ins, sv alignment, double-width runes — is counted once.
	over := disp >= width && (col < len(runes) || hi < len(hints))
	return b.String(), over
}

// leapStyles returns the label and match styles of the label-jump overlay
// (#787); both stay zero while no jump is active.
func (m Model) leapStyles(active bool) (label, match lipgloss.Style) {
	if !active {
		return label, match
	}
	label = lipgloss.NewStyle().Foreground(m.theme().Surface).Background(m.theme().Accent).Bold(true)
	match = lipgloss.NewStyle().Background(m.theme().SelectionMuted)
	return label, match
}

// lineHintsWithLens returns the line's inlay hints plus its code lens (#1912)
// as one synthetic trailing hint. The hint slice is copied before appending so
// the cached one is never mutated.
func (m Model) lineHintsWithLens(line int) []ilsp.InlayHint {
	hints := m.lineInlayHints(line)
	if lens, ok := m.lineLensHint(line); ok {
		hints = append(append([]ilsp.InlayHint(nil), hints...), lens)
	}
	return hints
}

// lineSearchSpans returns the active query's matches on the line. Only the
// rendered window [dlo, dhi) of a very long line is scanned (#2734).
func (m Model) lineSearchSpans(line, dlo, dhi int) []search.Span {
	q, ok := m.searchHLQuery()
	if !ok {
		return nil
	}
	return q.LineMatchesIn(m.buf, line, dlo, dhi)
}

// sgrSpansFor parses the SGR sequences of a line holding control runes (#1469).
func sgrSpansFor(runes []rune) []sgrSpan {
	if !hasCtrlRune(runes) {
		return nil
	}
	return parseSGRSpans(runes)
}

// svTintFor returns the caret column stripe tint (#1659), nil without a column.
func (m Model) svTintFor(has bool) color.Color {
	if !has {
		return nil
	}
	return m.svColumnTint()
}

// conflictCellStyle applies the merge-conflict styling (#1149): section
// backgrounds slot under the syntax colour like occurrences; marker lines
// render dim and bold, the diff3 base section dim. The role rides the line
// cache safely — blocks change only with the document version.
func (m Model) conflictCellStyle(line int, st lipgloss.Style, styled bool) (lipgloss.Style, bool) {
	switch m.conflictRoleOf(line) {
	case conflictOurs:
		return st.Background(theme.Mix(m.theme().VCSAdded, m.theme().Surface, conflictTintFrac)), true
	case conflictTheirs:
		return st.Background(theme.Mix(m.theme().VCSModified, m.theme().Surface, conflictTintFrac)), true
	case conflictBase:
		return st.Faint(true), true
	case conflictMarker:
		return st.Faint(true).Bold(true), true
	}
	return st, styled
}

// baseCellCtx bundles the per-span inputs baseCellStyle reads for every cell.
type baseCellCtx struct {
	sgrSpans []sgrSpan
	ctrl     lipgloss.Style
	hasSVCol bool
	svStart  int
	svEnd    int
	svTint   color.Color
	rulerBG  color.Color
}

// baseCellStyle resolves the syntax, whitespace-glyph, SGR, caret-column and
// ruler layers of a cell that no higher-priority overlay claimed.
func (m Model) baseCellStyle(line, col, abs int, overlay *lipgloss.Style, bc *baseCellCtx) (lipgloss.Style, bool) {
	st, styled := m.styleAt(line, col)
	if overlay != nil {
		// Whitespace glyphs / indent guides (#64) replace the syntax
		// colour; cursor/selection/search already won above.
		st, styled = *overlay, true
	}
	st, styled = applySGRSpan(bc.sgrSpans, col, st, styled, overlay != nil, bc.ctrl)
	if bc.hasSVCol && col >= bc.svStart && col < bc.svEnd {
		// Caret column stripe (#1659): the lowest background layer —
		// ruler, conflict and occurrence tints all paint over it;
		// cursor/selection/search already won above.
		st = st.Background(bc.svTint)
		styled = true
	}
	if m.onRuler(abs) {
		// Ruler tint (#64): a background stripe under everything the
		// higher-priority overlays didn't claim.
		st = st.Background(bc.rulerBG)
		styled = true
	}
	return st, styled
}

// renderStyledCell renders cell through st when any layer styled it.
func renderStyledCell(st lipgloss.Style, styled bool, cell string) string {
	if styled {
		return st.Render(cell)
	}
	return cell
}

// leapCellAt reports the label-jump state of one cell (#787): the label rune
// drawn over it, or whether it lies in the typed span of a target.
func (m Model) leapCellAt(active bool, line, col int) (r rune, label, match bool) {
	if !active {
		return 0, false, false
	}
	if lr, ok := m.leapLabelAt(line, col); ok {
		return lr, true, false
	}
	return 0, false, m.leapMatchAt(line, col)
}

// flushLineEnd emits the hints anchored at or past the line end, then the
// complexity trailer, and returns the updated display width and hint index.
func (m Model) flushLineEnd(b *strings.Builder, line, width, disp, hi int, hints []ilsp.InlayHint, hintStyle lipgloss.Style) (int, int) {
	for hi < len(hints) && disp < width {
		disp = emitInlayHint(b, hintStyle, width, disp, hints[hi])
		hi++
	}
	// The complexity hint (0550, #2919) comes last, after any code
	// lens, and only whole: it never truncates and never counts
	// toward the right-edge mark — a header with no room shows none.
	// A per-site mark (#2947) takes the same slot on the lines the
	// header hint leaves free.
	if hi >= len(hints) {
		if cx, w := m.complexityTrailer(line, width-disp); w > 0 {
			b.WriteString(cx)
			disp += w
		}
	}
	return disp, hi
}

// cellGlyphKit bundles the per-span styles and offsets cellGlyph reads.
type cellGlyphKit struct {
	ws, ctrl, invis       lipgloss.Style
	guides                []lipgloss.Style
	trailStart, indentEnd int
}

// cellGlyph picks the text and overlay style of one buffer cell: tabs,
// visible whitespace, indent guides, control bytes, placeholders, lone runes
// and grapheme clusters. w is the cell's display width from the cell table.
func (m Model) cellGlyph(runes []rune, col, end, abs, w int, k *cellGlyphKit) (cell string, cells int, isTab bool, overlay *lipgloss.Style) {
	cell, cells = " ", 1
	switch r := runes[col]; {
	case r == '\t':
		isTab = true
		cell, cells = strings.Repeat(" ", m.tabWidth), m.tabWidth
		if m.wsVisible(col, k.trailStart) {
			cell = "→" + strings.Repeat(" ", cells-1)
			overlay = &k.ws
		} else if m.guideAt(col, k.indentEnd, abs) {
			cell = "│" + strings.Repeat(" ", cells-1)
			overlay = &k.guides[guideSlot(abs, m.tabWidth, len(k.guides))]
		}
	case r == ' ' && m.wsVisible(col, k.trailStart):
		cell = "·"
		overlay = &k.ws
	case r == ' ' && m.guideAt(col, k.indentEnd, abs):
		cell = "│"
		overlay = &k.guides[guideSlot(abs, m.tabWidth, len(k.guides))]
	case isCtrlRune(r):
		// Control byte (#1469): a one-cell placeholder glyph, never
		// the raw byte — one buffer rune stays one display cell, so
		// click mapping and caret rendering stay aligned.
		cell = ctrlGlyph(r)
		overlay = &k.ctrl
	case end == col+1:
		if g, ok := unihint.Placeholder(r); ok {
			// Invisible/format rune (#1654): the one-cell placeholder,
			// never the raw (zero-width) rune.
			cell = g
			overlay = &k.invis
		} else {
			// A lone rune, possibly double-width (#2526).
			cell, cells = string(r), w
		}
	default:
		// A grapheme cluster (#2526): the whole sequence — base, skin
		// tone, joiners, combining marks — goes to the terminal as one
		// string, budgeted at the cluster's width. cleanCluster already
		// kept placeholder runes out of here.
		cell, cells = string(runes[col:end]), w
	}
	return cell, cells, isTab, overlay
}

// cellFlags is the overlay state of one cell, tested by renderOverlayCell.
type cellFlags struct {
	leapHere, leapMatch, cursorHere, caretHere bool
	selected, inCurrent, inMatch, inFilter     bool
	isTab                                      bool
	leapRune                                   rune
	cell                                       string
	cells                                      int
}

// overlayStyles holds the styles of the overlays that outrank syntax colour.
type overlayStyles struct {
	cursor, caret, leapLabel, leapMatch lipgloss.Style
	sel, curMatch, match, filter        lipgloss.Style
}

// renderOverlayCell renders a cell claimed by a higher-priority overlay, in
// priority order. ok is false when none claims it and syntax styling applies.
func renderOverlayCell(f cellFlags, s *overlayStyles) (out string, ok bool) {
	switch {
	case f.leapHere:
		// Label jump (#787): the label glyph replaces the cell, above even
		// the cursor — it is what the next key acts on. On a tab only the
		// first cell carries the glyph, like the cursor.
		out = s.leapLabel.Render(string(f.leapRune))
		if f.cells > 1 {
			out += strings.Repeat(" ", f.cells-1)
		}
		return out, true
	case f.cursorHere:
		return renderCaretCell(s.cursor, f.cell, f.isTab, f.cells), true
	case f.caretHere:
		return renderCaretCell(s.caret, f.cell, f.isTab, f.cells), true
	case f.leapMatch:
		// The typed span of a label-jump target (#787), below the cursor
		// and carets but above the selection and search styling.
		return s.leapMatch.Render(f.cell), true
	case f.selected:
		return s.sel.Render(f.cell), true
	case f.inCurrent:
		return s.curMatch.Render(f.cell), true
	case f.inMatch:
		return s.match.Render(f.cell), true
	case f.inFilter:
		// Below the search overlays, above the syntax colour (#2255).
		return s.filter.Render(f.cell), true
	}
	return "", false
}

// firstHintFrom returns the index of the first hint anchored at or after col.
func firstHintFrom(hints []ilsp.InlayHint, col int) int {
	hi := 0
	for hi < len(hints) && hints[hi].Col < col {
		hi++
	}
	return hi
}

// emitInlayHint writes one inlay hint at display offset disp, truncated to the
// room left before the right edge, and returns the new display offset.
func emitInlayHint(b *strings.Builder, style lipgloss.Style, width, disp int, h ilsp.InlayHint) int {
	text := hintText(h)
	if w := width - disp; lipgloss.Width(text) > w {
		text = truncate(text, w)
	}
	b.WriteString(style.Render(text))
	return disp + lipgloss.Width(text)
}

// trailingWSStart returns the first column of the trailing-whitespace run
// (#64), or len(runes) when whitespace display is off.
func (m Model) trailingWSStart(runes []rune) int {
	start := len(runes)
	if m.wsMode == wsNone {
		return start
	}
	for start > 0 && (runes[start-1] == ' ' || runes[start-1] == '\t') {
		start--
	}
	return start
}

// leadingIndentEnd returns the end of the leading indent (#64), or 0 when
// indent guides are off.
func (m Model) leadingIndentEnd(runes []rune) int {
	end := 0
	if !m.indentGuides {
		return end
	}
	for end < len(runes) && (runes[end] == ' ' || runes[end] == '\t') {
		end++
	}
	return end
}

// onRuler reports whether cell is a ruler column.
func (m Model) onRuler(cell int) bool {
	for _, r := range m.rulers {
		if r == cell {
			return true
		}
	}
	return false
}

// padRulerTail pads blanks past the content end (#64) so a ruler reads as a
// continuous stripe on short lines. Only the unbounded final span (to < 0)
// pads — wrapped middle segments are already full width. It returns the new
// display offset.
func (m Model) padRulerTail(b *strings.Builder, to, abs, disp, width int, rulerBG color.Color) int {
	if to >= 0 || len(m.rulers) == 0 {
		return disp
	}
	maxRuler := 0
	for _, r := range m.rulers {
		if r > maxRuler {
			maxRuler = r
		}
	}
	rulerStyle := lipgloss.NewStyle().Background(rulerBG)
	for ; disp < width && abs <= maxRuler; abs++ {
		if m.onRuler(abs) {
			b.WriteString(rulerStyle.Render(" "))
		} else {
			b.WriteString(" ")
		}
		disp++
	}
	return disp
}

// writeStandIn emits a conceal range's stand-in at display offset disp
// (#1585) and returns the new offset. Its tint is the decoded-stand-in
// background, or the caret column stripe when the cell sits in it (#1659).
func (m Model) writeStandIn(b *strings.Builder, line, col, width, disp int, repl string, standIn, inSV bool, svTint color.Color) int {
	w := lipgloss.Width(repl)
	if w == 0 {
		return disp
	}
	if disp+w > width {
		// The stand-in straddles the right edge (#1847): render the prefix
		// that fits, exactly like a tab clipping there, so the text after the
		// range cannot slide left into the cells the alignment padding owns.
		repl = clipCells(repl, width-disp)
		w = lipgloss.Width(repl)
	}
	st, _ := m.styleAt(line, col)
	switch {
	case standIn:
		st = st.Background(m.theme().SelectionMuted)
	case inSV:
		st = st.Background(svTint)
	}
	b.WriteString(st.Render(repl))
	return disp + w
}

// searchMatchAt reports whether col lies in a search match and whether that
// match is the one under the cursor (#255, #2145).
func (m Model) searchMatchAt(line, col int, spans []search.Span) (inMatch, inCurrent bool) {
	for _, s := range spans {
		if col >= s.Start && col < s.End {
			return true, line == m.cursor.Line && m.cursor.Col >= s.Start && m.cursor.Col < s.End
		}
	}
	return false, false
}

// renderCaretCell draws a cursor or caret cell. On a tab only the first cell
// takes the style (it may carry a whitespace/guide glyph) and the rest stays
// plain; a wide glyph (#2526) instead highlights whole — one glyph, both cells.
func renderCaretCell(style lipgloss.Style, cell string, isTab bool, cells int) string {
	if isTab && cells > 1 {
		return style.Render(string([]rune(cell)[0])) + strings.Repeat(" ", cells-1)
	}
	return style.Render(cell)
}

// applySGRSpan folds an interpreted SGR sequence (#1469) into the cell style:
// the sequence's own runes dim like the ␛ glyph; the text it governs takes the
// file's own colour/attributes in place of the syntax colour, unless an overlay
// glyph already replaced it.
func applySGRSpan(spans []sgrSpan, col int, st lipgloss.Style, styled, overlaid bool, ctrlStyle lipgloss.Style) (lipgloss.Style, bool) {
	sp, ok := sgrSpanAt(spans, col)
	if !ok {
		return st, styled
	}
	if sp.Seq {
		return ctrlStyle, true
	}
	if !overlaid {
		return sp.apply(st), true
	}
	return st, styled
}

// markCellStyle layers the occurrence mark (#172) and the pre-selected snippet
// placeholder (#2146) backgrounds under the syntax colour.
func (m Model) markCellStyle(line, col int, st lipgloss.Style, styled bool) (lipgloss.Style, bool) {
	if kind, ok := m.occurrenceAt(line, col); ok {
		st, styled = st.Background(m.occurrenceColor(kind)), true
	}
	if m.snippetSelAt(line, col) {
		st, styled = st.Background(m.theme().SelectionMuted), true
	}
	return st, styled
}

// finishCellStyle composes the diagnostic underline over the cell style and
// the stale-content faint cue (#2777) over everything else.
func (m Model) finishCellStyle(line, col int, st lipgloss.Style, styled bool) (lipgloss.Style, bool) {
	if sev, ok := m.diagSeverityAt(line, col); ok {
		st, styled = st.Underline(true).UnderlineColor(m.diagColor(sev)), true
	}
	if m.dimmed {
		st, styled = st.Faint(true), true
	}
	return st, styled
}

// clipCells cuts s to at most w display cells (#1847): the hard cut a run of
// buffer cells would take at the right edge, without truncate's ellipsis — a
// conceal stand-in is layout, and marking it as shortened would misplace the
// cells that follow it. A wide rune straddling the edge yields to spaces, the
// same yield the cell loop gives a clipped tab.
func clipCells(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > w {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString(strings.Repeat(" ", w-used))
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
// expansion, concealed columns (#881/#1585) and injected inlay hints (#171) —
// so overlays anchored at a buffer cell align with what renderLine actually
// drew.
func (m Model) DisplayOffset(line, col int) int {
	runes := []rune(m.buf.Line(line))
	from := m.view.Left
	if m.softWrap {
		// Under soft wrap (#64) the offset counts from the cell's own wrap
		// segment; DisplayRow supplies the matching row.
		segs := m.wrapSegs(line)
		from = segs[viewport.SegmentIndex(segs, col)]
	}
	// A box-drawn table row (#881) maps through the cell layout (#1599).
	if d, ok := m.tableDisplayCol(line, col); ok {
		return d - from
	}
	// A table-rendered sv buffer scrolls in display space (#1724): the anchor
	// sits at its expanded column minus the display window start.
	if m.svActive() {
		return m.svDisplayCol(line, col) - from
	}
	conceals := m.lineConcealRanges(line)
	cw := m.lineCells(runes)
	// A caret on an absorbed column anchors on the glyph it belongs to (#2526).
	col = cw.head(runes, col)
	disp := 0
	for c := from; c < col; c++ {
		if cr, ok := rangeAt(conceals, c); ok {
			// A concealed column emits nothing; a replacement range emits
			// its stand-in's cells once, at the range start.
			if cr.repl != "" && c == cr.start {
				disp += lipgloss.Width(cr.repl)
			}
			continue
		}
		// Cell layout (#2526): a wide glyph counts two, a column absorbed
		// into a cluster none — so a caret on an absorbed column anchors on
		// the glyph it belongs to.
		disp += cw.at(runes, c, m.tabWidth)
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

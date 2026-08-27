package editor

// fold.go implements code folding (#144): collapsing the body of a function,
// block, or import list behind its header line. The foldable ranges come from
// the same Tree-sitter parse that produces the highlight spans
// (highlight.HighlightScoped, kinds from the language's FoldNodes), merged
// with any server-provided folding ranges via foldRanges (#1912, lspfold.go);
// this file owns the per-view set of collapsed folds and everything that
// treats a collapsed fold as one display row — vertical motions, scrolling,
// mouse mapping — plus the vim fold commands (za zc zo zM zR).
//
// Folding is a view concern: with shared documents (#142) each pane folds
// independently, so `folded` lives on the Model like the cursor and is reset
// when a pane becomes a new view of a document.

import (
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
	"ike/internal/editor/motion"
	"ike/internal/editor/operator"
	"ike/internal/editor/register"
	"ike/internal/highlight"
)

// hasFolds reports whether this view hides any line — a collapsed fold, a
// collapsed log repeat run (#1650), a summarised PEM block (#1652) or a line
// the follow filter narrowed away (#2255) — the fast gate the
// render/motion/scroll paths check before doing fold-aware work.
func (m Model) hasFolds() bool {
	return len(m.folded) > 0 || m.hasLogRuns() || m.hasPemBlocks() || m.logFilterHiding()
}

// lineHidden reports whether line is inside a collapsed fold body (the header
// line itself stays visible), folded away inside a log repeat run, inside a
// collapsed PEM block, or filtered out of a followed stream (#2255).
func (m Model) lineHidden(line int) bool {
	if m.logFilterHidden(line) || m.logRunHidden(line) || m.pemHidden(line) {
		return true
	}
	if len(m.folded) == 0 {
		return false
	}
	heads, ends := m.foldSpans()
	// The last header strictly above line; a fold hides line when any fold at
	// or before that header reaches it, which is exactly the running maximum
	// of the ends up to that index.
	i := sort.SearchInts(heads, line) - 1
	return i >= 0 && ends[i] >= line
}

// foldSpans returns the collapsed set as an interval index: the header lines
// ascending, and the running maximum of the fold ends beside them. lineHidden
// used to scan the whole map per rendered row per frame — a 100k-line file
// under zM is ~400k map iterations per frame between the View body loop and
// wrap.DisplayRow (#2187) — so the index is built once per fold mutation
// (foldEpoch) and shared across the Model's value copies via foldIdx.
func (m Model) foldSpans() (heads, ends []int) {
	if c := m.foldIdx; c != nil && c.valid && c.epoch == m.foldEpoch && c.n == len(m.folded) {
		return c.heads, c.ends
	}
	heads = make([]int, 0, len(m.folded))
	for h := range m.folded {
		heads = append(heads, h)
	}
	sort.Ints(heads)
	ends = make([]int, len(heads))
	best := 0
	for i, h := range heads {
		if e := m.folded[h]; i == 0 || e > best {
			best = e
		}
		ends[i] = best
	}
	if c := m.foldIdx; c != nil {
		*c = foldIndex{epoch: m.foldEpoch, n: len(m.folded), heads: heads, ends: ends, valid: true}
	}
	return heads, ends
}

// foldIndex memoizes the interval index of the collapsed set across Model
// value copies (#2187); the single-threaded update loop is the only writer.
// n guards the epoch against the copies that share a folded map: a mutation
// that changed the size can never be served under a stale epoch.
type foldIndex struct {
	epoch int
	n     int
	heads []int
	ends  []int
	valid bool
}

// bumpFolds invalidates the fold interval index. Every mutation of folded
// funnels through it — the index is only as trustworthy as that funnel.
func (m *Model) bumpFolds() { m.foldEpoch++ }

// foldedAt returns the end line of the collapsed fold whose header is line.
func (m Model) foldedAt(line int) (int, bool) {
	e, ok := m.folded[line]
	return e, ok
}

// collapsedRow reports whether line renders as exactly one row because it
// heads a collapsed fold, a collapsed log repeat run (#1650) or a summarised
// PEM block (#1652) — the soft-wrap paths must not slice such a line into wrap
// segments.
func (m Model) collapsedRow(line int) bool {
	if _, ok := m.foldedAt(line); ok {
		return true
	}
	if _, ok := m.logRunAt(line); ok {
		return true
	}
	_, ok := m.pemBlockAt(line)
	return ok
}

// expandFoldedLines grows the last line of the linewise span a..z so it covers
// the hidden body of every collapsed fold whose header the span already covers
// (#1741): a closed fold is one display row, so a linewise operation over that
// row must act on the whole fold, like vim. Folds nest and may chain, so the
// scan repeats until nothing grows — a fold revealed by an earlier expansion
// gets its own body pulled in too. z only ever grows and is bounded by the
// largest fold end, so the loop terminates.
func (m Model) expandFoldedLines(a, z int) int {
	for grew := true; grew; {
		grew = false
		for h, e := range m.folded {
			if h >= a && h <= z && e > z {
				z, grew = e, true
			}
		}
	}
	if last := m.buf.LineCount() - 1; z > last {
		z = last
	}
	return z
}

// expandFoldTarget widens a linewise operator target over the collapsed folds
// it covers (#1741). Charwise targets pass through untouched: a selection
// inside one visible line never means "the whole fold". Folds live on the view,
// not the buffer, so the operator package cannot do this itself — the expansion
// happens here, before the target is handed over.
func (m Model) expandFoldTarget(t operator.Target) operator.Target {
	if !t.Linewise || len(m.folded) == 0 {
		return t
	}
	a, z := t.Range.Start.Line, t.Range.End.Line
	if z < a {
		a, z = z, a
	}
	end := m.expandFoldedLines(a, z)
	if end == z {
		return t
	}
	return operator.LineTarget(a, end)
}

// visibleStep returns the next visible line from line in direction dir (+1
// down, -1 up), skipping collapsed fold bodies. ok is false at the buffer
// edge (no visible line remains in that direction).
func (m Model) visibleStep(line, dir int) (int, bool) {
	lc := m.buf.LineCount()
	n := line + dir
	for n >= 0 && n < lc && m.lineHidden(n) {
		n += dir
	}
	if n < 0 || n >= lc {
		return line, false
	}
	return n, true
}

// foldVertical is motion.Down/Up made fold-aware: each count steps one
// visible line, so a collapsed fold moves past as a single row (vim's j/k
// over closed folds).
func (m Model) foldVertical(count, dir int) motion.Result {
	line := m.cursor.Line
	for i := 0; i < count; i++ {
		n, ok := m.visibleStep(line, dir)
		if !ok {
			break
		}
		line = n
	}
	return motion.Result{Pos: buffer.Position{Line: line, Col: m.cursor.Col}, Kind: motion.Linewise}
}

// innermostOpenFold returns the innermost not-yet-collapsed fold containing
// line. Repeated zc therefore closes outward one level at a time, like vim.
func (m Model) innermostOpenFold(line int) (highlight.Fold, bool) {
	var best highlight.Fold
	found := false
	for _, f := range m.foldRanges() {
		if !f.Contains(line) {
			continue
		}
		if _, closed := m.folded[f.HeaderLine]; closed {
			continue
		}
		if !found || f.HeaderLine >= best.HeaderLine {
			best, found = f, true
		}
	}
	return best, found
}

// foldToggle is za: open the collapsed fold on the cursor's header line, or
// collapse the innermost fold containing the cursor.
func (m *Model) foldToggle() {
	if _, ok := m.folded[m.cursor.Line]; ok {
		m.foldOpenAtCursor()
		return
	}
	m.foldCloseAtCursor()
}

// foldCloseAtCursor is zc: collapse the innermost open fold containing the
// cursor; the cursor moves onto the header so it never sits on a hidden line.
func (m *Model) foldCloseAtCursor() {
	f, ok := m.innermostOpenFold(m.cursor.Line)
	if !ok {
		return
	}
	if m.folded == nil {
		m.folded = make(map[int]int)
	}
	m.folded[f.HeaderLine] = f.EndLine
	m.bumpFolds()
	if m.cursor.Line != f.HeaderLine {
		m.moveTo(buffer.Position{Line: f.HeaderLine, Col: m.cursor.Col})
	}
}

// foldOpenAtCursor is zo: open the collapsed fold on the cursor's header
// line. Folds collapsed inside it stay collapsed, so opening reveals one
// level, like vim.
func (m *Model) foldOpenAtCursor() {
	if _, ok := m.folded[m.cursor.Line]; ok {
		delete(m.folded, m.cursor.Line)
		m.bumpFolds()
		return
	}
	// Defensive: a cursor inside a collapsed body (programmatic placement)
	// opens the innermost fold hiding it.
	best := -1
	for h, e := range m.folded {
		if m.cursor.Line > h && m.cursor.Line <= e && h > best {
			best = h
		}
	}
	if best >= 0 {
		delete(m.folded, best)
		m.bumpFolds()
	}
}

// foldCloseAll is zM: collapse every foldable range. The first (outermost)
// fold on a header line wins; the cursor snaps out of hidden bodies onto the
// nearest enclosing header.
func (m *Model) foldCloseAll() {
	folds := m.foldRanges()
	if len(folds) == 0 {
		return
	}
	if m.folded == nil {
		m.folded = make(map[int]int, len(folds))
	}
	for _, f := range folds {
		if _, ok := m.folded[f.HeaderLine]; !ok {
			m.folded[f.HeaderLine] = f.EndLine
		}
	}
	m.bumpFolds()
	m.snapCursorOut()
}

// foldOpenAll is zR: open every fold.
func (m *Model) foldOpenAll() {
	m.folded = nil
	m.bumpFolds()
}

// snapCursorOut moves a cursor hidden inside a collapsed fold onto the
// innermost enclosing header. Each pass moves the cursor to a smaller line
// (the header above it), so the loop terminates.
func (m *Model) snapCursorOut() {
	for m.lineHidden(m.cursor.Line) {
		best := -1
		for h, e := range m.folded {
			if m.cursor.Line > h && m.cursor.Line <= e && h > best {
				best = h
			}
		}
		if best < 0 {
			return
		}
		m.moveTo(buffer.Position{Line: best, Col: m.cursor.Col})
	}
}

// unfoldAtCursor opens every collapsed fold hiding the cursor line. It runs
// from scroll() — the choke point every update funnels through — so any jump
// into a fold (search landing, G, go-to-definition, an edit elsewhere syncing
// the cursor) auto-unfolds it, vim's foldopen behaviour. Fold commands keep
// their fold closed by parking the cursor on the header first.
func (m *Model) unfoldAtCursor() {
	if len(m.folded) == 0 {
		return
	}
	for h, e := range m.folded {
		if m.cursor.Line > h && m.cursor.Line <= e {
			delete(m.folded, h)
			m.bumpFolds()
		}
	}
}

// dissolveFoldsAtEdit keeps collapsed folds consistent across a buffer
// mutation, called from emit(EventChange) with the cursor at the edit site:
// a fold the edit lands in (or on the header of) dissolves; folds below the
// edit shift by the line delta; anything pushed out of range drops. The next
// parse result reconciles the survivors against the fresh fold ranges
// (reconcileFolds), so stale ranges never outlive a reparse — the same
// version-gated lifecycle as the highlight spans.
func (m *Model) dissolveFoldsAtEdit() {
	lc := m.buf.LineCount()
	delta := lc - m.foldLines
	m.foldLines = lc
	if len(m.folded) == 0 {
		return
	}
	edit := m.cursor.Line
	next := make(map[int]int, len(m.folded))
	for h, e := range m.folded {
		switch {
		case edit >= h && edit <= e:
			// Edit inside or on the fold: dissolve it.
		case h > edit:
			h, e = h+delta, e+delta
			if h > edit && e < lc {
				next[h] = e
			}
		default:
			next[h] = e
		}
	}
	m.folded = next
	if len(m.folded) == 0 {
		m.folded = nil
	}
	m.bumpFolds()
}

// reconcileFolds re-anchors this view's collapsed folds against the fold
// ranges of a freshly accepted parse: a collapsed header that still starts a
// fold keeps it (end updated), anything else dissolves. Runs when a SpansMsg
// passes the version guard, so folds are version-gated exactly like spans.
func (m *Model) reconcileFolds() {
	m.foldLines = m.buf.LineCount()
	if len(m.folded) == 0 {
		return
	}
	folds := m.foldRanges()
	valid := make(map[int]int, len(folds))
	for _, f := range folds {
		if _, ok := valid[f.HeaderLine]; !ok {
			valid[f.HeaderLine] = f.EndLine
		}
	}
	for h := range m.folded {
		if e, ok := valid[h]; ok {
			m.folded[h] = e
		} else {
			delete(m.folded, h)
		}
	}
	m.bumpFolds()
}

// resetFolds clears both the fold ranges and this view's collapsed set, for
// the load/share paths that reset the highlight caches.
func (m *Model) resetFolds() {
	m.folds = nil
	m.lspFolds = nil  // server ranges belong to the previous document (#1912)
	m.hostFolds = nil // host ranges likewise (#2029): the host reinstalls them
	m.folded = nil
	m.bumpFolds()
	m.foldLines = m.buf.LineCount()
}

// displayLineAt maps a viewport row offset (n rows below view.Top) to the
// buffer line rendered there, counting each collapsed fold as one row — the
// mouse map's inverse of the View() render loop.
func (m Model) displayLineAt(n int) int {
	line := m.view.Top
	lc := m.buf.LineCount()
	for line < lc-1 && m.lineHidden(line) {
		line++
	}
	for ; n > 0; n-- {
		nx, ok := m.visibleStep(line, 1)
		if !ok {
			break
		}
		line = nx
	}
	return line
}

// foldScrollFix refines the viewport's cursor-follow scroll when folds are
// collapsed: viewport.Scroll counts buffer lines, so a window spanning folds
// holds more content than its height suggests and Top would move too early.
// This recounts the Top→cursor distance in visible rows and advances Top only
// while the cursor is actually below the last visible row. It also lifts a
// Top that landed inside a collapsed body up onto its header.
func (m *Model) foldScrollFix() {
	if !m.hasFolds() {
		return
	}
	for m.view.Top > 0 && m.lineHidden(m.view.Top) {
		m.view.Top--
	}
	h := m.view.Height()
	if h <= 0 || m.cursor.Line < m.view.Top {
		return
	}
	rows := 1
	for l := m.view.Top; l < m.cursor.Line; rows++ {
		n, ok := m.visibleStep(l, 1)
		if !ok || n > m.cursor.Line {
			break
		}
		l = n
	}
	for rows > h {
		n, ok := m.visibleStep(m.view.Top, 1)
		if !ok {
			return
		}
		m.view.Top = n
		rows--
	}
}

// foldCopyGlyph is the copy affordance a collapsed fold header carries
// (#1787): clicking it puts the whole hidden range on the system clipboard, so
// reading a folded JSON object out of a file never needs an unfold plus a drag.
const foldCopyGlyph = "⧉"

// foldTag is the "⋯ N lines" placeholder of a collapsed header — or whatever
// a host's fold summary (#2029, hostfold.go) names the node instead, which is
// how the jq result window says "3 keys" where a file says "3 lines".
func (m Model) foldTag(line, end int) string {
	if m.foldSummary != nil {
		if s := m.foldSummary(line, end); s != "" {
			return " " + s
		}
	}
	return " ⋯ " + strconv.Itoa(end-line) + " lines"
}

// annotColumnWidth is the width right-aligned row annotations budget against:
// the text width, minus the column the overlaid scrollbar claims (#1728).
func (m Model) annotColumnWidth() int {
	w := m.view.TextWidth(m.buf.LineCount())
	if _, _, _, _, ok := m.scrollbarGeometry(); ok {
		w--
	}
	return w
}

// foldCopyCell reports where the copy affordance of the collapsed fold headed
// by line is drawn: the offset of its first cell from the text area's left
// edge, and its width. ok is false when the row has no room for it — a very
// narrow pane, where the placeholder alone already fills the annotation column.
//
// Render and hit test share this one function, so the clickable cell is by
// construction the cell the glyph occupies.
func (m Model) foldCopyCell(line, end int) (off, width int, ok bool) {
	annot := m.annotColumnWidth()
	gw := lipgloss.Width(foldCopyGlyph)
	if annot <= 0 || annot-gw-1 <= lipgloss.Width(m.foldTag(line, end)) {
		return 0, 0, false
	}
	return annot - gw, gw, true
}

// renderFoldHeader renders the header line of a collapsed fold: the line
// content with a dimmed placeholder carrying the hidden-line count appended,
// budgeted so the row stays within the text width, plus the copy affordance
// right-aligned into the annotation column (#1787) — the column the ×N badges
// and blame hints share, so the markers of a file stay one scannable column and
// the affordance's hit target is a fixed cell instead of drifting with the
// header's length.
func (m Model) renderFoldHeader(line, end, width int, cursorStyle, selStyle lipgloss.Style) string {
	tag := m.foldTag(line, end)
	tw := lipgloss.Width(tag)
	faint := lipgloss.NewStyle().Faint(true)
	off, _, ok := m.foldCopyCell(line, end)
	if !ok {
		// No room for the affordance: the placeholder alone, as before.
		if tw >= width {
			return m.renderLine(line, width, cursorStyle, selStyle)
		}
		return m.renderLine(line, width-tw, cursorStyle, selStyle) + faint.Render(tag)
	}
	body := m.renderLine(line, off-tw, cursorStyle, selStyle) + faint.Render(tag)
	body = ansi.Truncate(body, off, "")
	if pad := off - ansi.StringWidth(body); pad > 0 {
		body += strings.Repeat(" ", pad)
	}
	return body + lipgloss.NewStyle().Foreground(m.theme().Secondary).Render(foldCopyGlyph)
}

// FoldCopyHit reports the buffer line whose collapsed-fold copy affordance sits
// under the content-local cell (x, y) — the mouse target that copies a fold
// without unfolding it (#1787). Sticky-scroll rows are excluded: they render
// through renderLine and carry no affordance.
func (m *Model) FoldCopyHit(x, y int) (int, bool) {
	if len(m.folded) == 0 || y < 0 {
		return 0, false
	}
	if y < len(m.stickyLines()) {
		return 0, false
	}
	line := m.clickPosition(x, y).Line
	end, ok := m.foldedAt(line)
	if !ok {
		return 0, false
	}
	off, gw, ok := m.foldCopyCell(line, end)
	if !ok {
		return 0, false
	}
	cell := x - m.view.GutterWidth(m.buf.LineCount())
	return line, cell >= off && cell < off+gw
}

// CopyFoldAt copies the collapsed fold headed by line — header through end
// line, raw buffer text — onto the system clipboard (#1787). nil when line
// heads no collapsed fold.
func (m *Model) CopyFoldAt(line int) tea.Cmd {
	end, ok := m.foldedAt(line)
	if !ok {
		return nil
	}
	return m.copyLineRange(line, end)
}

// foldCopyRange is the range the "Copy Folded Range" command acts on: the
// collapsed fold the cursor's line heads, else the innermost fold containing
// the cursor — open or closed, so the command also serves as "yank this block"
// without a fold ever being collapsed.
func (m Model) foldCopyRange() (start, end int, ok bool) {
	if e, closed := m.foldedAt(m.cursor.Line); closed {
		return m.cursor.Line, e, true
	}
	var best highlight.Fold
	for _, f := range m.foldRanges() {
		if !f.Contains(m.cursor.Line) {
			continue
		}
		if !ok || f.HeaderLine >= best.HeaderLine {
			best, ok = f, true
		}
	}
	return best.HeaderLine, best.EndLine, ok
}

// foldCopy runs the "Copy Folded Range" command (palette, zy): the fold under
// the cursor goes to the system clipboard whole, hidden lines included.
func (m *Model) foldCopy() tea.Cmd {
	start, end, ok := m.foldCopyRange()
	if !ok {
		return notice("no fold at the cursor")
	}
	return m.copyLineRange(start, end)
}

// copyLineRange yanks the linewise range start..end into the system-clipboard
// register `+` and returns the feedback toast. The text is the raw buffer
// content: conceals and stand-ins are display-only, so what lands on the
// clipboard is what the file holds — the copy semantics of every other yank.
func (m *Model) copyLineRange(start, end int) tea.Cmd {
	if start < 0 {
		start = 0
	}
	if last := m.buf.LineCount() - 1; end > last {
		end = last
	}
	if start > end {
		return nil
	}
	var b strings.Builder
	for l := start; l <= end; l++ {
		b.WriteString(m.buf.Line(l))
		b.WriteByte('\n')
	}
	m.regs.Yank('+', register.Entry{Text: b.String(), Linewise: true})
	return m.clipboardNotice("copied")
}

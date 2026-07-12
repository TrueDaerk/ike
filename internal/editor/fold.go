package editor

// fold.go implements code folding (#144): collapsing the body of a function,
// block, or import list behind its header line. The foldable ranges come from
// the same Tree-sitter parse that produces the highlight spans
// (highlight.HighlightScoped, kinds from the language's FoldNodes); this file
// owns the per-view set of collapsed folds and everything that treats a
// collapsed fold as one display row — vertical motions, scrolling, mouse
// mapping — plus the vim fold commands (za zc zo zM zR).
//
// Folding is a view concern: with shared documents (#142) each pane folds
// independently, so `folded` lives on the Model like the cursor and is reset
// when a pane becomes a new view of a document.

import (
	"strconv"

	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/motion"
	"ike/internal/highlight"
)

// hasFolds reports whether this view has any collapsed fold, the fast gate
// the render/motion/scroll paths check before doing fold-aware work.
func (m Model) hasFolds() bool { return len(m.folded) > 0 }

// lineHidden reports whether line is inside a collapsed fold body (the header
// line itself stays visible).
func (m Model) lineHidden(line int) bool {
	for h, e := range m.folded {
		if line > h && line <= e {
			return true
		}
	}
	return false
}

// foldedAt returns the end line of the collapsed fold whose header is line.
func (m Model) foldedAt(line int) (int, bool) {
	e, ok := m.folded[line]
	return e, ok
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
	for _, f := range m.folds {
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
	}
}

// foldCloseAll is zM: collapse every foldable range. The first (outermost)
// fold on a header line wins; the cursor snaps out of hidden bodies onto the
// nearest enclosing header.
func (m *Model) foldCloseAll() {
	if len(m.folds) == 0 {
		return
	}
	if m.folded == nil {
		m.folded = make(map[int]int, len(m.folds))
	}
	for _, f := range m.folds {
		if _, ok := m.folded[f.HeaderLine]; !ok {
			m.folded[f.HeaderLine] = f.EndLine
		}
	}
	m.snapCursorOut()
}

// foldOpenAll is zR: open every fold.
func (m *Model) foldOpenAll() { m.folded = nil }

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
	valid := make(map[int]int, len(m.folds))
	for _, f := range m.folds {
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
}

// resetFolds clears both the fold ranges and this view's collapsed set, for
// the load/share paths that reset the highlight caches.
func (m *Model) resetFolds() {
	m.folds = nil
	m.folded = nil
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

// renderFoldHeader renders the header line of a collapsed fold: the line
// content with a dimmed placeholder carrying the hidden-line count appended,
// budgeted so the row stays within the text width.
func (m Model) renderFoldHeader(line, end, width int, cursorStyle, selStyle lipgloss.Style) string {
	tag := " ⋯ " + strconv.Itoa(end-line) + " lines"
	tw := lipgloss.Width(tag)
	if tw >= width {
		return m.renderLine(line, width, cursorStyle, selStyle)
	}
	body := m.renderLine(line, width-tw, cursorStyle, selStyle)
	return body + lipgloss.NewStyle().Faint(true).Render(tag)
}

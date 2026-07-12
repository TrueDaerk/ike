package editor

import (
	"strconv"
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/editor/motion"
	"ike/internal/editor/viewport"
)

// wrap.go holds the soft-wrap plumbing (#64): the buffer-facing side of the
// viewport wrap map (segments per line, rows per line), visual-row cursor
// motion for j/k, the wrapped mouse-click mapping, and the view-option
// toggles the palette commands flip.

// wrapSegs returns the wrap segments of line at the current text width.
func (m Model) wrapSegs(line int) []int {
	return viewport.WrapSegments([]rune(m.buf.Line(line)), m.view.TextWidth(m.buf.LineCount()), m.tabWidth)
}

// wrapRows reports how many screen rows line occupies under soft wrap: 0 when
// hidden inside a collapsed fold, 1 for a collapsed fold's header (it renders
// as a single row), else its wrap-segment count.
func (m Model) wrapRows(line int) int {
	if m.lineHidden(line) {
		return 0
	}
	if _, ok := m.foldedAt(line); ok {
		return 1
	}
	return len(m.wrapSegs(line))
}

// wrapVertical is the soft-wrap j/k motion: one step per visual row (vim's
// gj/gk), keeping the cursor's offset within the row where the target row is
// long enough. Line transitions step over collapsed folds like foldVertical.
// The result is charwise, so an operator composing with it (d j) spans
// characters — matching vim's d gj.
func (m *Model) wrapVertical(count, dir int) motion.Result {
	line, col := m.cursor.Line, m.cursor.Col
	segs := m.wrapSegs(line)
	si := viewport.SegmentIndex(segs, col)
	off := col - segs[si]
	for ; count > 0; count-- {
		if dir > 0 && si+1 < len(segs) {
			si++
			continue
		}
		if dir < 0 && si > 0 {
			si--
			continue
		}
		next, ok := m.visibleStep(line, dir)
		if !ok {
			break
		}
		line = next
		segs = m.wrapSegs(line)
		if dir > 0 {
			si = 0
		} else {
			si = len(segs) - 1
		}
	}
	col = segs[si] + off
	if end := viewport.SegmentEnd(segs, si, len([]rune(m.buf.Line(line)))); col > end-1 && end > segs[si] {
		col = end - 1
	}
	return motion.Result{Pos: buffer.Position{Line: line, Col: col}, Kind: motion.Exclusive}
}

// wrapClickAt maps a clicked content row y to the buffer line and the column
// start of the visual row rendered there — the mouse map's inverse of the
// wrapped View() loop. Rows covered by sticky-scroll headers map to the first
// body line (the caller overrides them with the header's declaration line).
func (m Model) wrapClickAt(y int) (line, segStart int) {
	lc := m.buf.LineCount()
	n := y - m.stickyCount()
	if n < 0 {
		n = 0
	}
	line = m.view.Top + m.stickyCount()
	if line > lc-1 {
		line = lc - 1
	}
	for line < lc-1 && m.lineHidden(line) {
		line++
	}
	for line < lc {
		rows := m.wrapRows(line)
		if n < rows {
			if _, folded := m.foldedAt(line); !folded {
				segs := m.wrapSegs(line)
				return line, segs[n]
			}
			return line, 0
		}
		n -= rows
		next, ok := m.visibleStep(line, 1)
		if !ok {
			break
		}
		line = next
	}
	return line, 0
}

// DisplayRow returns the screen row (relative to the first body row) where the
// buffer cell (line, col) renders, counting collapsed folds as one row and, under
// soft wrap, each wrap segment as its own row. Overlay anchoring (LSP popups)
// composes it with DisplayOffset for the full screen position.
func (m Model) DisplayRow(line, col int) int {
	if !m.softWrap && !m.hasFolds() {
		return line - m.view.Top
	}
	row := 0
	for l := m.view.Top; l < line; l++ {
		if m.softWrap {
			row += m.wrapRows(l)
			continue
		}
		if !m.lineHidden(l) {
			row++
		}
	}
	if m.softWrap {
		if _, folded := m.foldedAt(line); !folded {
			row += viewport.SegmentIndex(m.wrapSegs(line), col)
		}
	}
	return row
}

// parseRulers parses the comma-separated editor.rulers value ("80,120") the
// config layer flattens the TOML list into. Invalid or non-positive entries
// are dropped (config validation already warned).
func parseRulers(v string) []int {
	if v == "" {
		return nil
	}
	var out []int
	for _, part := range strings.Split(v, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// toggleWrap flips soft wrap for this view (view.toggleWrap). The override
// sticks across config refreshes; horizontal scroll resets so the wrapped
// view starts at the line's left edge.
func (m *Model) toggleWrap() {
	m.softWrap = !m.softWrap
	m.wrapSet = true
	m.view.Left = 0
	m.scroll()
}

// toggleWhitespace flips whitespace rendering for this view
// (view.toggleWhitespace): off switches to "all"; any visible mode switches
// off. The configured "trailing" mode returns on the next config refresh only
// if the view never toggled (the override sticks).
func (m *Model) toggleWhitespace() {
	if m.wsMode == wsNone {
		m.wsMode = wsAll
	} else {
		m.wsMode = wsNone
	}
	m.wsSet = true
}

// toggleIndentGuides flips indent guides for this view (view.toggleIndentGuides).
func (m *Model) toggleIndentGuides() {
	m.indentGuides = !m.indentGuides
	m.guidesSet = true
}

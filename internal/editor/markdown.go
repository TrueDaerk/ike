package editor

import (
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/highlight"
)

// markdown.go is the Markdown rich-rendering layer (#881), vim-conceal style:
// inline emphasis renders with terminal text attributes, marker chrome
// (**, *, `, link []() parts) is hidden on lines the cursor is not on, and
// pipe tables re-render with box-drawing characters while the cursor is
// outside the table block. The buffer never changes — this is display only;
// the cursor line always shows raw source so editing stays exact.
//
// The conceal data rides the ordinary highlight pipeline: the markdown inline
// query captures the chrome as @conceal, and the SpansMsg handler splits those
// spans into m.conceal (per-line column ranges) instead of the style index.
// Tables are detected from the buffer text (a delimiter row under a pipe row)
// — equivalent to the grammar's pipe_table extent, but it also works in
// CGO_ENABLED=0 builds and needs no extra parse channel.

// concealSplit separates @conceal spans from the style spans of a parse
// result. The conceal ranges are per-line [start, end) rune columns, in span
// order (the query emits them left-to-right per line).
func concealSplit(spans []highlight.Span) (style []highlight.Span, conceal map[int][][2]int) {
	for _, s := range spans {
		if s.Capture == "conceal" {
			if conceal == nil {
				conceal = make(map[int][][2]int)
			}
			conceal[s.Line] = append(conceal[s.Line], [2]int{s.StartCol, s.EndCol})
			continue
		}
		style = append(style, s)
	}
	return style, conceal
}

// concealOn reports whether concealment applies to line right now: the toggle
// is on, the line has conceal ranges, and neither the cursor (of a focused
// view) nor any secondary caret sits on it — those lines always show raw
// source.
func (m Model) concealOn(line int) bool {
	if !m.mdRender || len(m.conceal[line]) == 0 {
		return false
	}
	if m.focused && line == m.cursor.Line {
		return false
	}
	for _, c := range m.carets {
		if c.pos.Line == line {
			return false
		}
	}
	return true
}

// concealedAt reports whether the rune column on line falls in a conceal range.
func (m Model) concealedAt(line, col int) bool {
	for _, r := range m.conceal[line] {
		if col >= r[0] && col < r[1] {
			return true
		}
	}
	return false
}

// concealClickCol is the mouse map's inverse for a concealed line (#881): the
// clicked offset counts display cells, which skip concealed columns, so the
// buffer column is the one whose unconcealed-prefix length matches. Columns at
// or past the line end map 1:1 (nothing left to conceal).
func (m Model) concealClickCol(line, from, offset int) int {
	runes := len([]rune(m.buf.Line(line)))
	col := from
	for ; col < runes; col++ {
		if m.concealedAt(line, col) {
			continue
		}
		if offset == 0 {
			return col
		}
		offset--
	}
	return col + offset
}

// --- pipe tables -----------------------------------------------------------

// mdTableBlock is one detected pipe table: the inclusive source line range and
// one pre-rendered display row per source line (same count — the delimiter row
// renders as the ├─┼─┤ separator, so line↔row mapping stays trivial and the
// gutter never reflows).
type mdTableBlock struct {
	start, end int
	rows       []string
}

// mdTableState caches detected tables per document version. A pointer field on
// Model, like lineCache, so the value copies each Update returns share it.
type mdTableState struct {
	version int
	valid   bool
	blocks  []mdTableBlock
}

// mdTableRow returns the pre-rendered display row for line when table
// rendering applies: markdown document, toggle on, no soft wrap (a box-drawn
// row sliced by raw-text wrap segments would tear), and the cursor outside the
// block — entering it flips the whole block back to raw pipe source.
func (m Model) mdTableRow(line int) (string, bool) {
	if !m.mdRender || m.softWrap || m.mdTables == nil {
		return "", false
	}
	for _, b := range m.tableBlocks() {
		if line < b.start || line > b.end {
			continue
		}
		if m.focused && m.cursor.Line >= b.start && m.cursor.Line <= b.end {
			return "", false
		}
		for _, c := range m.carets {
			if c.pos.Line >= b.start && c.pos.Line <= b.end {
				return "", false
			}
		}
		return b.rows[line-b.start], true
	}
	return "", false
}

// tableBlocks returns the document's pipe tables, recomputing only when the
// document version moved.
func (m Model) tableBlocks() []mdTableBlock {
	st := m.mdTables
	if st.valid && st.version == m.docVersion {
		return st.blocks
	}
	st.blocks = nil
	if highlight.Lang(m.path) == "markdown" {
		st.blocks = detectTables(m.buf.Lines())
	}
	st.version, st.valid = m.docVersion, true
	return st.blocks
}

// detectTables scans lines for pipe tables: a row line (cells between |)
// directly above a delimiter row (only -, :, |, spaces — with at least one -),
// plus every consecutive row line below.
func detectTables(lines []string) []mdTableBlock {
	var blocks []mdTableBlock
	for i := 0; i+1 < len(lines); i++ {
		if !isPipeRow(lines[i]) || !isDelimiterRow(lines[i+1]) {
			continue
		}
		end := i + 1
		for end+1 < len(lines) && isPipeRow(lines[end+1]) && !isDelimiterRow(lines[end+1]) {
			end++
		}
		blocks = append(blocks, renderTable(lines, i, end))
		i = end
	}
	return blocks
}

// isPipeRow reports whether a line is a table row: after indent it starts
// with | (the unambiguous GFM form — cells without a leading pipe are not
// claimed, better a missed table than a false one).
func isPipeRow(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "|") && len(t) > 1
}

// isDelimiterRow reports whether a line is the header/body separator:
// pipes around runs of - with optional : alignment colons.
func isDelimiterRow(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "|") {
		return false
	}
	dash := false
	for _, r := range t {
		switch r {
		case '|', ':', ' ', '\t':
		case '-':
			dash = true
		default:
			return false
		}
	}
	return dash
}

// splitCells splits a table row into trimmed cell texts, honoring \| escapes.
func splitCells(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	var cells []string
	var b strings.Builder
	esc := false
	for _, r := range t {
		switch {
		case esc:
			if r != '|' {
				b.WriteRune('\\')
			}
			b.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == '|':
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if esc {
		b.WriteRune('\\')
	}
	cells = append(cells, strings.TrimSpace(b.String()))
	return cells
}

const (
	alignLeft = iota
	alignCenter
	alignRight
)

// renderTable pre-renders the block's display rows: cells padded to the
// column's max display width, aligned per the delimiter row, pipes replaced by
// │ and the delimiter row by ├─┼─┤.
func renderTable(lines []string, start, end int) mdTableBlock {
	rows := make([][]string, 0, end-start+1)
	cols := 0
	for i := start; i <= end; i++ {
		cells := splitCells(lines[i])
		if len(cells) > cols {
			cols = len(cells)
		}
		rows = append(rows, cells)
	}

	aligns := make([]int, cols)
	for ci, cell := range rows[1] { // rows[1] is the delimiter row by detection
		t := strings.TrimSpace(cell)
		switch {
		case strings.HasPrefix(t, ":") && strings.HasSuffix(t, ":"):
			aligns[ci] = alignCenter
		case strings.HasSuffix(t, ":"):
			aligns[ci] = alignRight
		}
	}

	widths := make([]int, cols)
	for ri, cells := range rows {
		if ri == 1 {
			continue // the delimiter row does not size columns
		}
		for ci, cell := range cells {
			if w := lipgloss.Width(cell); w > widths[ci] {
				widths[ci] = w
			}
		}
	}

	b := mdTableBlock{start: start, end: end}
	for ri, cells := range rows {
		if ri == 1 {
			var s strings.Builder
			s.WriteRune('├')
			for ci, w := range widths {
				if ci > 0 {
					s.WriteRune('┼')
				}
				s.WriteString(strings.Repeat("─", w+2))
			}
			s.WriteRune('┤')
			b.rows = append(b.rows, s.String())
			continue
		}
		var s strings.Builder
		s.WriteRune('│')
		for ci, w := range widths {
			if ci > 0 {
				s.WriteRune('│')
			}
			cell := ""
			if ci < len(cells) {
				cell = cells[ci]
			}
			pad := w - lipgloss.Width(cell)
			left, right := 0, pad
			switch aligns[ci] {
			case alignRight:
				left, right = pad, 0
			case alignCenter:
				left, right = pad/2, pad-pad/2
			}
			s.WriteString(" " + strings.Repeat(" ", left) + cell + strings.Repeat(" ", right) + " ")
		}
		s.WriteRune('│')
		b.rows = append(b.rows, s.String())
	}
	return b
}

// renderTableRow emits the [from, to) column window of a pre-rendered table
// row, box glyphs faint, padded/truncated to width — the table path's stand-in
// for renderSpanUncached's cell loop.
func (m Model) renderTableRow(row string, from, to, width int) string {
	runes := []rune(row)
	if from > len(runes) {
		from = len(runes)
	}
	end := len(runes)
	if to >= 0 && to < end {
		end = to
	}
	border := lipgloss.NewStyle().Faint(true)
	var b strings.Builder
	disp := 0
	for _, r := range runes[from:end] {
		if disp >= width {
			break
		}
		switch r {
		case '│', '├', '┼', '┤', '─':
			b.WriteString(border.Render(string(r)))
		default:
			b.WriteRune(r)
		}
		disp++
	}
	return b.String()
}

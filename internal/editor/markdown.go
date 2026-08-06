package editor

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/cronhint"
	"ike/internal/epochtime"
	"ike/internal/escapes"
	"ike/internal/highlight"
	"ike/internal/numhint"
	"ike/internal/secret"
)

// markdown.go is the Markdown rich-rendering layer (#881), vim-conceal style:
// inline emphasis renders with terminal text attributes, marker chrome
// (**, *, `, link []() parts) is hidden, and pipe tables re-render with
// box-drawing characters. The buffer never changes — this is display only; a
// concealed range shows raw source exactly while the caret sits inside it (or
// a selection crosses it, #1594) or inside its enclosing inline span (#1599),
// and a table reveals only the caret's cell (#1599), so editing stays exact.
//
// The conceal data rides the ordinary highlight pipeline: the markdown inline
// query captures the chrome as @conceal, and the SpansMsg handler splits those
// spans into m.conceal (per-line column ranges) instead of the style index.
// Tables are detected from the buffer text (a delimiter row under a pipe row)
// — equivalent to the grammar's pipe_table extent, but it also works in
// CGO_ENABLED=0 builds and needs no extra parse channel.

// concealRange is one concealed [start, end) rune-column range on a line. An
// empty repl hides the range outright (markdown marker chrome, #881); a
// non-empty repl renders as a stand-in for the source runes. standIn marks a
// decoded value ("%20" displays as " ", #1585) — tinted so it reads as a
// stand-in, not buffer content; the sv separator padding (#1589) is plain
// spacing and renders untinted.
type concealRange struct {
	start, end int
	repl       string
	standIn    bool
}

// decodeCaptures are the capture names of the decode conceal families that
// ride their own channels, each gated by its own toggle: decoded epoch
// timestamps (#1618) and the #1620 escape families (unicode escapes, HTML/XML
// entities, base64 Secret values), plus the secret masking family (#1623),
// which conceals rather than decodes but rides the same channel. The slice
// order is the per-line combine order in lineConcealRanges — fixed, so
// rendering is deterministic.
var decodeCaptures = []string{
	epochtime.Capture,
	escapes.UnicodeCapture,
	escapes.EntityCapture,
	escapes.Base64Capture,
	cronhint.Capture,
	numhint.SizeCapture,
	numhint.DurationCapture,
	numhint.GroupCapture,
	numhint.RadixCapture,
	secret.Capture,
}

func isDecodeCapture(capture string) bool {
	for _, c := range decodeCaptures {
		if c == capture {
			return true
		}
	}
	return false
}

// concealSplit separates conceal spans — the @conceal capture (#881), the
// @conceal.extent span extents (#1599) and any span carrying a Replace
// stand-in (#1585) — from the style spans of a parse result. The conceal
// ranges are per-line, in span order (producers emit them left-to-right per
// line). A Replace span keeps its style span too: the raw source (cursor
// line, selection) still styles by its capture. Extents are the enclosing
// inline spans (emphasis, code span, link): a caret anywhere inside one
// reveals the conceal ranges it contains, so the whole span reads raw while
// edited (see lineConcealRanges). Decode-family spans (#1618 timestamps,
// #1620 escapes) split into decodes, keyed by capture: they ride the same
// stand-in mechanic but each family toggles on its own switch, independent of
// the markdown and log layers.
func concealSplit(spans []highlight.Span) (style []highlight.Span, conceal, extents map[int][]concealRange, decodes map[string]map[int][]concealRange) {
	add := func(m *map[int][]concealRange, s highlight.Span) {
		if *m == nil {
			*m = make(map[int][]concealRange)
		}
		(*m)[s.Line] = append((*m)[s.Line], concealRange{
			start: s.StartCol, end: s.EndCol, repl: s.Replace, standIn: s.Replace != "",
		})
	}
	for _, s := range spans {
		switch {
		case s.Capture == "conceal":
			add(&conceal, s)
			continue
		case s.Capture == "conceal.extent":
			add(&extents, s)
			continue
		case s.Replace != "" && isDecodeCapture(s.Capture):
			if decodes == nil {
				decodes = make(map[string]map[int][]concealRange)
			}
			fam := decodes[s.Capture]
			add(&fam, s)
			decodes[s.Capture] = fam
		case s.Replace != "":
			add(&conceal, s)
		}
		style = append(style, s)
	}
	return style, conceal, extents, decodes
}

// lineConcealRanges returns the conceal ranges applying to line right now:
// the parse-produced ranges (markdown marker chrome #881, decoded stand-ins
// #1585 — gated by the markdown-rendering toggle), the decode families
// (epoch timestamps #1618, escaped text #1620 — each gated by its own
// editor.*_decoding toggle) plus the dynamic sv separator ranges (#1589,
// gated by editor.csv_rendering), with every range the caret sits inside —
// or a selection intersects — dropped, so exactly that spot shows raw source
// while the rest of the line stays rendered (#1594, vim's concealcursor
// granularity).
func (m Model) lineConcealRanges(line int) []concealRange {
	var ranges []concealRange
	if m.parseConcealOn() {
		ranges = m.conceal[line]
	}
	for _, c := range decodeCaptures {
		if ds := m.decodes[c][line]; m.decodeOn(c) && len(ds) > 0 {
			// Never alias m.conceal's backing array when combining.
			ranges = append(append([]concealRange(nil), ranges...), ds...)
		}
	}
	if sv := m.svConcealRanges(line); len(sv) > 0 {
		ranges = append(append([]concealRange(nil), ranges...), sv...)
	}
	if len(ranges) == 0 {
		return nil
	}
	cursorCol, cursorOn := -1, false
	if m.focused && line == m.cursor.Line {
		cursorCol, cursorOn = m.cursor.Col, true
	}
	selStart, selEnd, hasSel := m.selectionOnLine(line, len([]rune(m.buf.Line(line))))
	inRange := func(r concealRange) bool {
		if cursorOn && cursorCol >= r.start && cursorCol < r.end {
			return true
		}
		for _, c := range m.carets {
			if c.pos.Line == line && c.pos.Col >= r.start && c.pos.Col < r.end {
				return true
			}
		}
		// The selection range is inclusive; the conceal range half-open.
		return hasSel && r.start <= selEnd && r.end > selStart
	}
	// Span extents (#1599): a caret anywhere inside an inline span — not only
	// on a marker — reveals the markers that span contains, so the whole span
	// reads raw while edited.
	var hot []concealRange
	if m.mdRender {
		for _, e := range m.concealExt[line] {
			if inRange(e) {
				hot = append(hot, e)
			}
		}
	}
	revealed := func(r concealRange) bool {
		if inRange(r) {
			return true
		}
		for _, e := range hot {
			if r.start >= e.start && r.end <= e.end {
				return true
			}
		}
		return false
	}
	anyRevealed := false
	for _, r := range ranges {
		if revealed(r) {
			anyRevealed = true
			break
		}
	}
	if !anyRevealed {
		return ranges
	}
	// Build a fresh slice — ranges may alias the cached m.conceal entry.
	out := make([]concealRange, 0, len(ranges)-1)
	for _, r := range ranges {
		if !revealed(r) {
			out = append(out, r)
		}
	}
	return out
}

// toggleMarkdownRendering flips markdown rich rendering for this view
// (view.toggleMarkdownRendering, #1599): conceal, inline attributes and table
// drawing all key on mdRender, so one flag switches the whole layer between
// rendered and raw source. The override sticks like the #64 view toggles.
func (m *Model) toggleMarkdownRendering() {
	m.mdRender = !m.mdRender
	m.mdRenderSet = true
}

// rangeAt returns the conceal range covering the rune column, if any.
func rangeAt(ranges []concealRange, col int) (concealRange, bool) {
	for _, r := range ranges {
		if col >= r.start && col < r.end {
			return r, true
		}
	}
	return concealRange{}, false
}

// --- pipe tables -----------------------------------------------------------

// mdTableBlock is one detected pipe table: the inclusive source line range and
// one pre-rendered display row per source line (same count — the delimiter row
// renders as the ├─┼─┤ separator, so line↔row mapping stays trivial and the
// gutter never reflows). widths are the per-column content widths the rows
// were laid out with (each column's display region is width+2, plus one-cell
// borders) — the display↔source column mapping (#1599) keys on them.
type mdTableBlock struct {
	start, end int
	rows       []string
	widths     []int
}

// cellSeg is one cell's source segment on a pipe row: the [from, to) rune
// columns between the enclosing pipes, the pipes themselves excluded.
type cellSeg struct{ from, to int }

// rowCellSegs parses a pipe row into its cell segments, honoring \| escapes.
// A trailing all-space segment after the closing pipe is not a cell
// (splitCells trims it the same way). nil when the line is no pipe row.
func rowCellSegs(line string) []cellSeg {
	runes := []rune(line)
	i := 0
	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}
	if i >= len(runes) || runes[i] != '|' {
		return nil
	}
	var segs []cellSeg
	start, esc := i+1, false
	for j := i + 1; j < len(runes); j++ {
		switch {
		case esc:
			esc = false
		case runes[j] == '\\':
			esc = true
		case runes[j] == '|':
			segs = append(segs, cellSeg{from: start, to: j})
			start = j + 1
		}
	}
	if start < len(runes) && strings.TrimSpace(string(runes[start:])) != "" {
		segs = append(segs, cellSeg{from: start, to: len(runes)})
	}
	return segs
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
// row sliced by raw-text wrap segments would tear). With the cursor inside
// the block the frame stays box-drawn and only the cursor's cell reveals raw
// (#1599, tableRowsAt); secondary carets or a selection inside the block
// still flip it fully raw.
func (m Model) mdTableRow(line int) (string, bool) {
	rows, rawLine, b, ok := m.tableRowsAt(line)
	if !ok || line == rawLine {
		return "", false
	}
	return rows[line-b.start], true
}

// tableRowsAt resolves the box-drawn display of the table block containing
// line: the display rows, the block (carrying the widths the rows were laid
// out with), and rawLine — the one block line showing raw pipe source (-1
// when none; the cursor on table chrome or the delimiter row reveals its
// whole row). ok=false when line is in no block or the block renders fully
// raw: rendering off, soft wrap, secondary carets or a selection inside the
// block (their styling only renders through the raw cell loop).
func (m Model) tableRowsAt(line int) (rows []string, rawLine int, b mdTableBlock, ok bool) {
	if !m.mdRender || m.softWrap || m.mdTables == nil {
		return nil, 0, mdTableBlock{}, false
	}
	for _, blk := range m.tableBlocks() {
		if line < blk.start || line > blk.end {
			continue
		}
		for _, c := range m.carets {
			if c.pos.Line >= blk.start && c.pos.Line <= blk.end {
				return nil, 0, mdTableBlock{}, false
			}
		}
		if s, e, sel := m.selectionSpansLines(); sel && s <= blk.end && e >= blk.start {
			return nil, 0, mdTableBlock{}, false
		}
		if m.focused && m.cursor.Line >= blk.start && m.cursor.Line <= blk.end {
			return m.tableCursorRows(blk)
		}
		return blk.rows, -1, blk, true
	}
	return nil, 0, mdTableBlock{}, false
}

// selectionSpansLines returns the inclusive line range of the active visual
// selection (or the :s///c confirm highlight), ok=false when none is active.
func (m Model) selectionSpansLines() (start, end int, ok bool) {
	if sc := m.subConfirm; sc != nil {
		return sc.curLine, sc.curLine, true
	}
	if !m.mode.IsVisual() {
		return 0, 0, false
	}
	start, end = m.anchor.Line, m.cursor.Line
	if start > end {
		start, end = end, start
	}
	return start, end, true
}

// tableCursorRows renders blk with the cursor inside it (#1599): the frame
// stays box-drawn and only the cursor's cell shows raw source, re-laid-out so
// the column grows to fit the raw text. The cursor on the delimiter row or on
// table chrome (a pipe, the indent before the first pipe, past the last cell)
// reveals its whole row as raw source instead — reaching the cell edge
// de-conceals the borders.
func (m Model) tableCursorRows(blk mdTableBlock) (rows []string, rawLine int, b mdTableBlock, ok bool) {
	row := m.cursor.Line - blk.start
	if row == 1 {
		return blk.rows, m.cursor.Line, blk, true
	}
	cell, segs := m.rawCellAt(m.cursor.Line)
	if cell < 0 {
		return blk.rows, m.cursor.Line, blk, true
	}
	raw := &mdRawCell{
		row: row, cell: cell, seg: segs[cell],
		text:      []rune(m.buf.Line(m.cursor.Line)),
		cursorCol: m.cursor.Col, style: m.cursorStyle(),
	}
	nb := renderTableWith(m.buf.Lines(), blk.start, blk.end, m.cellStyles(), raw)
	return nb.rows, -1, nb, true
}

// rawCellAt returns the index of the cell revealed raw on line — the cell the
// cursor sits in (#1599) — or -1, plus the line's cell segments.
func (m Model) rawCellAt(line int) (int, []cellSeg) {
	segs := rowCellSegs(m.buf.Line(line))
	if m.focused && line == m.cursor.Line {
		for i, s := range segs {
			if m.cursor.Col >= s.from && m.cursor.Col < s.to {
				return i, segs
			}
		}
	}
	return -1, segs
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
		st.blocks = detectTables(m.buf.Lines(), m.cellStyles())
	}
	st.version, st.valid = m.docVersion, true
	return st.blocks
}

// mdCellStyles carries the theme styles cell-inline rendering needs (#945):
// inline code and link text keep the colors their captures get on ordinary
// lines (@string / @label); emphasis is pure text attributes.
type mdCellStyles struct {
	code  lipgloss.Style
	label lipgloss.Style
}

// cellStyles resolves the table cells' inline styles from the active theme.
func (m Model) cellStyles() mdCellStyles {
	var st mdCellStyles
	st.code, _ = m.hlTheme.Style("string")
	st.label, _ = m.hlTheme.Style("label")
	return st
}

// detectTables scans lines for pipe tables: a row line (cells between |)
// directly above a delimiter row (only -, :, |, spaces — with at least one -),
// plus every consecutive row line below.
func detectTables(lines []string, st mdCellStyles) []mdTableBlock {
	var blocks []mdTableBlock
	for i := 0; i+1 < len(lines); i++ {
		if !isPipeRow(lines[i]) || !isDelimiterRow(lines[i+1]) {
			continue
		}
		end := i + 1
		for end+1 < len(lines) && isPipeRow(lines[end+1]) && !isDelimiterRow(lines[end+1]) {
			end++
		}
		blocks = append(blocks, renderTable(lines, i, end, st))
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

// --- cell inline rendering (#945) ------------------------------------------
//
// The inline conceal/style pipeline runs per source line and column, so it
// cannot follow cell text into the re-laid-out box-drawing rows. Cells get a
// small self-contained renderer instead — like table detection it works
// without the CGo grammar: `code`, **bold**/__bold__, *italic*/_italic_,
// ~~strike~~ and [text](url) / ![alt](url), nesting via recursion, unmatched
// markers literal. The cursor's own cell renders raw via mdRawCell (#1599),
// so there is no cursor exception to honour here.

// renderCellInline renders one cell's inline markdown to a styled string.
func renderCellInline(text string, st mdCellStyles) string {
	return renderInlineSeg([]rune(text), lipgloss.NewStyle(), st)
}

// renderInlineSeg renders runes with base attributes, recursing into emphasis.
func renderInlineSeg(r []rune, base lipgloss.Style, st mdCellStyles) string {
	var out strings.Builder
	var lit []rune
	flush := func() {
		if len(lit) > 0 {
			out.WriteString(base.Render(string(lit)))
			lit = lit[:0]
		}
	}
	emit := func(styled string) {
		flush()
		out.WriteString(styled)
	}
	i := 0
	for i < len(r) {
		switch {
		case r[i] == '\\' && i+1 < len(r):
			lit = append(lit, r[i+1])
			i += 2
		case r[i] == '`':
			if j := indexFrom(r, i+1, "`"); j >= 0 {
				emit(st.code.Render(string(r[i+1 : j])))
				i = j + 1
				continue
			}
			lit = append(lit, r[i])
			i++
		case hasAt(r, i, "**") || hasAt(r, i, "__"):
			mk := string(r[i : i+2])
			if j := indexFrom(r, i+2, mk); j >= 0 && j > i+2 {
				j = slideToRunEnd(r, j)
				emit(renderInlineSeg(r[i+2:j], base.Bold(true), st))
				i = j + 2
				continue
			}
			lit = append(lit, r[i], r[i+1])
			i += 2
		case hasAt(r, i, "~~"):
			if j := indexFrom(r, i+2, "~~"); j >= 0 && j > i+2 {
				j = slideToRunEnd(r, j)
				emit(renderInlineSeg(r[i+2:j], base.Strikethrough(true), st))
				i = j + 2
				continue
			}
			lit = append(lit, r[i], r[i+1])
			i += 2
		case r[i] == '*':
			if j := indexFrom(r, i+1, "*"); j >= 0 && j > i+1 {
				emit(renderInlineSeg(r[i+1:j], base.Italic(true), st))
				i = j + 1
				continue
			}
			lit = append(lit, r[i])
			i++
		case r[i] == '_':
			// GFM: underscore emphasis only at word boundaries, so
			// snake_case_names stay literal.
			if i == 0 || !isAlnumRune(r[i-1]) {
				if j := indexFrom(r, i+1, "_"); j >= 0 && j > i+1 && (j+1 >= len(r) || !isAlnumRune(r[j+1])) {
					emit(renderInlineSeg(r[i+1:j], base.Italic(true), st))
					i = j + 1
					continue
				}
			}
			lit = append(lit, r[i])
			i++
		case r[i] == '[':
			if text, next, ok := parseCellLink(r, i); ok {
				emit(renderInlineSeg(text, st.label, st))
				i = next
				continue
			}
			lit = append(lit, r[i])
			i++
		case r[i] == '!' && i+1 < len(r) && r[i+1] == '[':
			if text, next, ok := parseCellLink(r, i+1); ok {
				emit(renderInlineSeg(text, st.label, st))
				i = next
				continue
			}
			lit = append(lit, r[i])
			i++
		default:
			lit = append(lit, r[i])
			i++
		}
	}
	flush()
	return out.String()
}

// parseCellLink matches [text](dest) starting at the [ and returns the text
// runes and the index past the closing parenthesis.
func parseCellLink(r []rune, i int) (text []rune, next int, ok bool) {
	close := indexFrom(r, i+1, "]")
	if close < 0 || close+1 >= len(r) || r[close+1] != '(' {
		return nil, 0, false
	}
	end := indexFrom(r, close+2, ")")
	if end < 0 {
		return nil, 0, false
	}
	return r[i+1 : close], end + 1, true
}

// slideToRunEnd shifts a found 2-char closing marker to the end of its
// delimiter run, so `**bold *nested***` closes the bold on the run's last two
// markers and the inner emphasis stays balanced (CommonMark resolves runs the
// same way).
func slideToRunEnd(r []rune, j int) int {
	for j+2 < len(r) && r[j+2] == r[j] {
		j++
	}
	return j
}

// hasAt reports whether the marker starts at index i.
func hasAt(r []rune, i int, marker string) bool {
	m := []rune(marker)
	if i+len(m) > len(r) {
		return false
	}
	for k, c := range m {
		if r[i+k] != c {
			return false
		}
	}
	return true
}

// indexFrom finds the marker at or after index from, skipping \-escapes;
// -1 when absent.
func indexFrom(r []rune, from int, marker string) int {
	for i := from; i < len(r); i++ {
		if r[i] == '\\' {
			i++
			continue
		}
		if hasAt(r, i, marker) {
			return i
		}
	}
	return -1
}

// isAlnumRune reports a letter or digit (ASCII fast path is enough for the
// underscore word-boundary rule).
func isAlnumRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// mdRawCell marks one cell to render as raw source (#1599): the
// block-relative row and cell index, the cell's source segment and row runes,
// and the cursor cell to style — the raw cell renders untrimmed and
// left-anchored, so source columns map 1:1 onto its display region.
type mdRawCell struct {
	row, cell int
	seg       cellSeg
	text      []rune
	cursorCol int
	style     lipgloss.Style
}

// display renders the raw source segment with the cursor cell styled.
func (rc *mdRawCell) display() string {
	r := rc.text[rc.seg.from:rc.seg.to]
	c := rc.cursorCol - rc.seg.from
	if c < 0 || c >= len(r) {
		return string(r)
	}
	return string(r[:c]) + rc.style.Render(string(r[c])) + string(r[c+1:])
}

// renderTable pre-renders the block's display rows: cells padded to the
// column's max display width, aligned per the delimiter row, pipes replaced by
// │ and the delimiter row by ├─┼─┤. Cell content renders its inline markdown
// (#945) — emphasis attributes, code/link theme styles, marker chrome dropped
// — so widths and alignment size by what is displayed, and the rows carry the
// styling (borders faint) ready for the ANSI-aware window in renderTableRow.
func renderTable(lines []string, start, end int, st mdCellStyles) mdTableBlock {
	return renderTableWith(lines, start, end, st, nil)
}

// renderTableWith is renderTable with an optional raw-revealed cell (#1599):
// that cell renders its raw source (untrimmed, left-anchored, cursor styled)
// instead of its inline-rendered form, and the widths grow to fit it.
func renderTableWith(lines []string, start, end int, st mdCellStyles, raw *mdRawCell) mdTableBlock {
	rows := make([][]string, 0, end-start+1)
	cols := 0
	for i := start; i <= end; i++ {
		cells := splitCells(lines[i])
		if i != start+1 { // the delimiter row stays raw
			for ci := range cells {
				if raw != nil && i-start == raw.row && ci == raw.cell {
					cells[ci] = raw.display()
					continue
				}
				cells[ci] = renderCellInline(cells[ci], st)
			}
		}
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

	border := lipgloss.NewStyle().Faint(true)
	pipe := border.Render("│")
	b := mdTableBlock{start: start, end: end, widths: widths}
	for ri, cells := range rows {
		if ri == 1 {
			var s strings.Builder
			s.WriteString(border.Render("├"))
			for ci, w := range widths {
				if ci > 0 {
					s.WriteString(border.Render("┼"))
				}
				s.WriteString(border.Render(strings.Repeat("─", w+2)))
			}
			s.WriteString(border.Render("┤"))
			b.rows = append(b.rows, s.String())
			continue
		}
		var s strings.Builder
		s.WriteString(pipe)
		for ci, w := range widths {
			if ci > 0 {
				s.WriteString(pipe)
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
			if raw != nil && ri == raw.row && ci == raw.cell {
				// The raw-revealed cell stays left-anchored so its source
				// columns map 1:1 onto the display region (#1599).
				left, right = 0, pad
			}
			s.WriteString(" " + strings.Repeat(" ", left) + cell + strings.Repeat(" ", right) + " ")
		}
		s.WriteString(pipe)
		b.rows = append(b.rows, s.String())
	}
	return b
}

// --- display↔source column mapping on box-drawn rows (#1599) ---------------

// tableCellStart returns the display column of cell ci's first padding cell
// (right after its left border) in a layout with the given widths: borders
// are one cell, each column's region is width+2.
func tableCellStart(widths []int, ci int) int {
	pos := 1
	for k := 0; k < ci && k < len(widths); k++ {
		pos += widths[k] + 3
	}
	return pos
}

// tableClickCol maps a click on a box-drawn table row to a source column: a
// border click lands on the pipe it draws, a cell click lands inside the
// cell's source segment — exact in the raw-revealed cursor cell, approximate
// (relative to the trimmed content) in rendered cells whose inline chrome is
// concealed. disp counts display cells from the line start; ok=false when the
// line does not currently render box-drawn.
func (m Model) tableClickCol(line, disp int) (int, bool) {
	_, rawLine, b, ok := m.tableRowsAt(line)
	if !ok || line == rawLine {
		return 0, false
	}
	rawCell, segs := m.rawCellAt(line)
	if len(segs) == 0 {
		return 0, false
	}
	runes := []rune(m.buf.Line(line))
	for ci := range b.widths {
		start := tableCellStart(b.widths, ci)
		si := ci
		if si >= len(segs) {
			si = len(segs) - 1
		}
		s := segs[si]
		if disp < start { // the border before this cell
			if s.from > 0 {
				return s.from - 1, true
			}
			return s.from, true
		}
		if disp < start+b.widths[ci]+2 || ci == len(b.widths)-1 {
			from := s.from
			if si != rawCell { // rendered cells display trimmed content
				for from < s.to && (runes[from] == ' ' || runes[from] == '\t') {
					from++
				}
			}
			col := from + disp - start - 1
			if col >= s.to {
				col = s.to - 1
			}
			if col < s.from {
				col = s.from
			}
			return col, true
		}
	}
	return len(runes), true
}

// tableDisplayCol maps a buffer column on a box-drawn table row to its
// display column from the line start — DisplayOffset's table branch,
// tableClickCol's inverse (same exact/approximate split).
func (m Model) tableDisplayCol(line, col int) (int, bool) {
	_, rawLine, b, ok := m.tableRowsAt(line)
	if !ok || line == rawLine {
		return 0, false
	}
	rawCell, segs := m.rawCellAt(line)
	if len(segs) == 0 {
		return 0, false
	}
	runes := []rune(m.buf.Line(line))
	for ci := range b.widths {
		if ci >= len(segs) {
			break
		}
		s := segs[ci]
		start := tableCellStart(b.widths, ci)
		if col < s.from { // on or before the pipe left of this cell
			return start - 1, true
		}
		if col < s.to || ci == len(b.widths)-1 || ci == len(segs)-1 {
			from := s.from
			if ci != rawCell {
				for from < s.to && (runes[from] == ' ' || runes[from] == '\t') {
					from++
				}
			}
			d := start + 1 + col - from
			if d < start+1 {
				d = start + 1
			}
			if last := start + b.widths[ci] + 1; d > last {
				d = last
			}
			return d, true
		}
	}
	return 0, false
}

// renderTableRow emits the [from, to) column window of a pre-rendered table
// row, truncated to width — the table path's stand-in for
// renderSpanUncached's cell loop. Rows carry their styling (cell inline
// styles, faint borders) from renderTable, so the window slices display cells
// ANSI-aware.
func (m Model) renderTableRow(row string, from, to, width int) string {
	end := from + width
	if to >= 0 && to < end {
		end = to
	}
	return ansi.Cut(row, from, end)
}

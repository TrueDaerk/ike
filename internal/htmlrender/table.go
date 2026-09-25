package htmlrender

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"ike/internal/hscroll"
)

// Tables (0530/4, #2742) render as a bordered grid in the box-drawing look of
// the markdown preview's tables: captions above, a rule under the header rows,
// "│" between cells. Every cell is laid out by a sub-render of the ordinary
// flow at its column's width (capture mode: lines are kept as runs instead of
// being written out), so links, images, anchors and the source map keep
// working inside cells, and a cell may hold paragraphs, lists or a nested
// table (flattened: a row per line, "│" between its cells). A first capture at
// the table's full width measures each cell — its widest line and its widest
// unbreakable word — and the columns are sized from those measurements.

// tableMinCol is the narrowest a column is squeezed to, unless its content
// is narrower still: room for a letter or two and the ellipsis.
const tableMinCol = 3

// Span caps, as HTML's: colspan at most 1000 columns, rowspan 65534 rows.
const (
	maxColSpan = 1000
	maxRowSpan = 65534
)

// cellLine is one captured line of a cell: its runs, the number of leading
// runs that are indent (list markers inside the cell), its width and the
// source offset of its content.
type cellLine struct {
	frags  []frag
	indent int
	w      int
	src    int
}

// gridCell is one cell of the laid-out grid. A nil n is filler: the space a
// rowspan reaches down into, or a row shorter than the table.
type gridCell struct {
	n         *html.Node
	col, span int
	rowspan   int
	lo, hi    int // measured: widest unbreakable run, widest line
	lines     []cellLine
}

// gridRow is one row of the grid, its cells covering every column in order.
type gridRow struct {
	n     *html.Node // the <tr>, nil for cells outside one
	off   int
	head  bool
	foot  bool
	cells []gridCell
}

// tableParts is a <table>'s content sorted out: the captions, the content
// that does not belong in any cell (rendered before the grid, as a browser
// fosters it), and the rows with their cell elements.
type tableParts struct {
	captions []*html.Node
	foster   []*html.Node
	rows     []gridRow
	open     bool // the last row is an implicit one stray cells may join
}

// collect sorts n's children into p; sect is the row group they sit in.
func (p *tableParts) collect(n *html.Node, sect string) {
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		switch k.Type {
		case html.TextNode:
			if strings.TrimSpace(k.Data) != "" {
				p.foster = append(p.foster, k)
			}
			continue
		case html.ElementNode:
		default:
			continue
		}
		if _, hidden := attr(k, "hidden"); hidden || skipped[k.Data] {
			continue
		}
		switch k.Data {
		case "caption":
			p.captions = append(p.captions, k)
		case "thead", "tbody", "tfoot":
			p.open = false
			p.collect(k, k.Data)
			p.open = false
		case "tr":
			p.rows = append(p.rows, gridRow{n: k, head: sect == "thead", foot: sect == "tfoot"})
			p.open = false
			p.collectCells(k, &p.rows[len(p.rows)-1])
		case "td", "th":
			if !p.open {
				p.rows = append(p.rows, gridRow{head: sect == "thead", foot: sect == "tfoot"})
				p.open = true
			}
			row := &p.rows[len(p.rows)-1]
			row.cells = append(row.cells, gridCell{n: k})
		case "table":
			p.foster = append(p.foster, k)
		default:
			p.collect(k, sect) // a wrapper (<form> around rows): transparent
		}
	}
}

// collectCells gathers a row's cells; wrappers between the row and its cells
// are transparent, other content is fostered out of the table.
func (p *tableParts) collectCells(n *html.Node, row *gridRow) {
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		switch k.Type {
		case html.TextNode:
			if strings.TrimSpace(k.Data) != "" {
				p.foster = append(p.foster, k)
			}
		case html.ElementNode:
			if _, hidden := attr(k, "hidden"); hidden || skipped[k.Data] {
				continue
			}
			switch k.Data {
			case "td", "th":
				row.cells = append(row.cells, gridCell{n: k})
			case "table", "caption", "tr":
				p.foster = append(p.foster, k)
			default:
				p.collectCells(k, row)
			}
		}
	}
}

// span reads a colspan/rowspan attribute: def when absent or invalid, capped
// at limit; zero passes through (rowspan=0 reaches the end of the table).
func span(n *html.Node, key string, def, limit int) int {
	v, ok := attr(n, key)
	if !ok {
		return def
	}
	s, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || s < 0 {
		return def
	}
	return min(s, limit)
}

// table renders a <table>. Inside a cell a nested table is flattened into
// the cell's flow instead: a row per line, its cells set off by "│".
func (r *renderer) table(n *html.Node, c ctx) {
	r.gap()
	if r.capturing {
		r.walk(n, c)
		r.gap()
		return
	}
	var p tableParts
	p.collect(n, "")
	for _, k := range p.foster {
		if k.Type == html.TextNode {
			r.text(k, c)
		} else {
			r.element(k, c)
		}
	}
	r.flush()
	for _, k := range p.captions {
		r.element(k, c)
	}
	rows, ncols := placeCells(r.t, p.rows)
	if ncols > 0 {
		r.grid(rows, ncols, c, r.t.start[n])
	}
	r.gap()
}

// placeCells assigns every cell its columns — the first free one, as HTML's
// table model does — moves the footer rows to the end, and fills each row up
// to the table's column count: where a rowspan reaches down, and where a row
// runs short, an empty cell stands.
func placeCells(t *tree, in []gridRow) ([]gridRow, int) {
	rows := make([]gridRow, 0, len(in))
	for _, row := range in {
		if !row.foot {
			rows = append(rows, row)
		}
	}
	for _, row := range in {
		if row.foot {
			rows = append(rows, row)
		}
	}
	// taken[i] marks the columns of row i covered by a rowspan from above;
	// reach records, per covered start, the filler's span.
	taken := make([][]bool, len(rows))
	fill := make([][]gridCell, len(rows))
	isTaken := func(i, col int) bool { return col < len(taken[i]) && taken[i][col] }
	ncols := 0
	for i := range rows {
		row := &rows[i]
		switch {
		case row.n != nil:
			row.off = t.start[row.n]
		case len(row.cells) > 0:
			row.off = t.start[row.cells[0].n]
		}
		col := 0
		for j := range row.cells {
			cell := &row.cells[j]
			for isTaken(i, col) {
				col++
			}
			if col >= maxColSpan {
				row.cells = row.cells[:j]
				break
			}
			cs := max(span(cell.n, "colspan", 1, maxColSpan), 1)
			cs = min(cs, maxColSpan-col)
			for k := 1; k < cs; k++ {
				if isTaken(i, col+k) {
					cs = k // a colspan ends where a rowspan from above sits
					break
				}
			}
			rs := span(cell.n, "rowspan", 1, maxRowSpan)
			if rs == 0 || rs > len(rows)-i {
				rs = len(rows) - i
			}
			cell.col, cell.span, cell.rowspan = col, cs, rs
			for k := i + 1; k < i+rs; k++ {
				if len(taken[k]) < col+cs {
					taken[k] = append(taken[k], make([]bool, col+cs-len(taken[k]))...)
				}
				for x := col; x < col+cs; x++ {
					taken[k][x] = true
				}
				fill[k] = append(fill[k], gridCell{col: col, span: cs})
			}
			col += cs
		}
		ncols = max(ncols, col, len(taken[i]))
	}
	for i := range rows {
		cells := append(rows[i].cells, fill[i]...)
		sortCells(cells)
		out := make([]gridCell, 0, len(cells))
		col := 0
		for _, cell := range cells {
			if cell.col < col {
				continue // overlaps an earlier cell: a malformed span
			}
			for ; col < cell.col; col++ {
				out = append(out, gridCell{col: col, span: 1})
			}
			out = append(out, cell)
			col = cell.col + cell.span
		}
		for ; col < ncols; col++ {
			out = append(out, gridCell{col: col, span: 1})
		}
		rows[i].cells = out
	}
	return rows, ncols
}

// sortCells orders a row's cells by column (insertion sort: rows are short
// and almost sorted — only the fillers join out of order).
func sortCells(cells []gridCell) {
	for i := 1; i < len(cells); i++ {
		for j := i; j > 0 && cells[j].col < cells[j-1].col; j-- {
			cells[j], cells[j-1] = cells[j-1], cells[j]
		}
	}
}

// grid sizes the columns, lays every cell out and emits the bordered rows;
// off is the table's source offset, where its top border maps.
func (r *renderer) grid(rows []gridRow, ncols int, c ctx, off int) {
	// Without a <thead>, leading rows of header cells only are the header.
	if !rows[0].head {
		for i := range rows {
			if rows[i].foot || !allHeaderCells(rows[i]) {
				break
			}
			rows[i].head = true
		}
	}
	avail := r.avail()
	full := max(1, avail-4) // the widest a cell can be: a one-column table
	for i := range rows {
		for j := range rows[i].cells {
			cell := &rows[i].cells[j]
			if cell.n == nil {
				continue
			}
			cell.lo, cell.hi = measure(r.cellLines(cell.n, r.cellCtx(rows[i], cell, c), full, true))
		}
	}
	widths := columnWidths(rows, ncols, avail-(3*ncols+1))
	// Body rows get a rule between them once any cell takes more than one
	// line: without it the lines of neighbouring rows run together.
	ruled := false
	for i := range rows {
		for j := range rows[i].cells {
			cell := &rows[i].cells[j]
			if cell.n == nil {
				continue
			}
			cell.lines = r.cellLines(cell.n, r.cellCtx(rows[i], cell, c), spanWidth(widths, *cell), false)
			ruled = ruled || len(cell.lines) > 1
		}
	}
	r.emit(r.rule(widths, nil, rows[0].cells, false), off)
	for i := range rows {
		if i > 0 {
			if head := rows[i-1].head && !rows[i].head; head || ruled {
				r.emit(r.rule(widths, rows[i-1].cells, rows[i].cells, head), -1)
			}
		}
		r.gridRow(rows[i], widths)
	}
	r.emit(r.rule(widths, rows[len(rows)-1].cells, nil, false), -1)
}

// allHeaderCells reports whether a row holds only <th> cells (and one at
// least).
func allHeaderCells(row gridRow) bool {
	seen := false
	for _, cell := range row.cells {
		if cell.n == nil {
			continue
		}
		if cell.n.Data != "th" {
			return false
		}
		seen = true
	}
	return seen
}

// cellCtx is the inline context a cell's content starts in: header rows and
// <th> cells are bold.
func (r *renderer) cellCtx(row gridRow, cell *gridCell, c ctx) ctx {
	if row.head || cell.n.Data == "th" {
		c.st = c.st.with(attrBold)
	}
	return c
}

// measure returns a cell's widest unbreakable run (indent included) and its
// widest line.
func measure(lines []cellLine) (lo, hi int) {
	for _, l := range lines {
		hi = max(hi, l.w)
		ind := 0
		for _, f := range l.frags[:l.indent] {
			ind += f.w
		}
		run := 0
		for _, f := range l.frags[l.indent:] {
			if f.kind == fSpace {
				run = 0
				continue
			}
			run += f.w
			lo = max(lo, ind+run)
		}
	}
	return lo, hi
}

// spanWidth is the width of a cell across its columns: the columns and the
// " │ " separators between them.
func spanWidth(widths []int, cell gridCell) int {
	w := 3 * (cell.span - 1)
	for _, cw := range widths[cell.col : cell.col+cell.span] {
		w += cw
	}
	return w
}

// columnWidths distributes avail content cells over the columns. Each column
// wants its widest line (hi) and needs its widest word (lo); a spanning cell's
// wants are spread over its columns. All wants fit: every column gets them.
// Else the needs fit: each gets its need, the rest grows the columns in
// proportion to what they still want. Else each gets the minimum width
// (tableMinCol, or less when its content is narrower) and the rest grows them
// toward their needs; content that still does not fit is cut with an
// ellipsis. A grid wider than the pane even at the minimum is cut at the
// right edge.
func columnWidths(rows []gridRow, ncols, avail int) []int {
	lo, hi := make([]int, ncols), make([]int, ncols)
	for _, row := range rows {
		for _, cell := range row.cells {
			if cell.n != nil && cell.span == 1 {
				lo[cell.col] = max(lo[cell.col], cell.lo)
				hi[cell.col] = max(hi[cell.col], cell.hi)
			}
		}
	}
	for _, row := range rows {
		for _, cell := range row.cells {
			if cell.n != nil && cell.span > 1 {
				spread(lo[cell.col:cell.col+cell.span], cell.lo-3*(cell.span-1))
				spread(hi[cell.col:cell.col+cell.span], cell.hi-3*(cell.span-1))
			}
		}
	}
	floor := make([]int, ncols)
	for i := range hi {
		hi[i] = max(hi[i], lo[i], 1)
		floor[i] = min(hi[i], tableMinCol)
		lo[i] = max(lo[i], floor[i])
	}
	switch {
	case sum(hi) <= avail:
		return hi
	case sum(lo) <= avail:
		return grow(lo, hi, avail-sum(lo))
	case sum(floor) <= avail:
		return share(floor, lo, avail)
	}
	return floor
}

// share splits avail cells fairly between columns that cannot all get their
// need: a column needing no more than an even share gets its need, the rest
// split what remains evenly — so short words stay whole and one long URL
// cannot starve its neighbours. No column drops below its floor.
func share(floor, need []int, avail int) []int {
	out := make([]int, len(need))
	open := len(need)
	done := make([]bool, len(need))
	for settled := true; settled && open > 0; {
		settled = false
		fair := avail / open
		for i := range need {
			if !done[i] && need[i] <= fair {
				out[i], done[i] = need[i], true
				avail -= need[i]
				open--
				settled = true
			}
		}
	}
	// The rest split evenly (never below the floor: sum(floor) fits, and a
	// floor is at most tableMinCol, below any share that left them open).
	rest := make([]int, 0, open)
	for i := range need {
		if !done[i] {
			rest = append(rest, i)
		}
	}
	for k, i := range rest {
		out[i] = max(avail/len(rest), floor[i])
		if k < avail%len(rest) {
			out[i]++
		}
	}
	return out
}

// spread widens cols evenly until together they hold need.
func spread(cols []int, need int) {
	if d := need - sum(cols); d > 0 {
		for i := range cols {
			cols[i] += d / len(cols)
			if i < d%len(cols) {
				cols[i]++
			}
		}
	}
}

// grow hands extra cells to the columns of w in proportion to how far each
// is from its target, never past it.
func grow(w, target []int, extra int) []int {
	want := 0
	for i := range w {
		want += target[i] - w[i]
	}
	if want <= extra {
		return target
	}
	out := make([]int, len(w))
	given := 0
	for i := range w {
		add := (target[i] - w[i]) * extra / want
		out[i] = w[i] + add
		given += add
	}
	for i := 0; given < extra; i = (i + 1) % len(out) {
		if out[i] < target[i] {
			out[i]++
			given++
		}
	}
	return out
}

func sum(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

// cellLines lays a cell's content out at width w in capture mode and returns
// the lines. The sub-render starts from a clean block state (no indent, no
// open list, no pending space or anchor) and hands the table's back
// afterwards. A measuring pass leaves no trace at all: the links and images
// it indexes are dropped again, as the real pass indexes them anew.
func (r *renderer) cellLines(n *html.Node, c ctx, w int, measuring bool) []cellLine {
	save := *r
	var outer linkBuild
	if c.link >= 0 {
		outer = r.links[c.link]
	}
	r.width, r.prefix, r.blank = w, nil, -1
	r.frags, r.space, r.anchors = nil, false, nil
	r.pre, r.preStart, r.lists, r.cells = 0, false, nil, nil
	r.capturing, r.measuring, r.captured = true, measuring, nil
	if id, _ := attr(n, "id"); id != "" {
		r.anchor(id)
	}
	r.walk(n, c)
	r.flush()
	if len(r.anchors) > 0 { // anchors after the last word land on its line
		if len(r.captured) == 0 {
			r.captured = append(r.captured, cellLine{src: -1})
		}
		l := &r.captured[len(r.captured)-1]
		l.frags = append(l.frags, frag{anchors: r.anchors, link: -1, img: -1, off: -1})
	}
	lines := r.captured
	if measuring {
		*r = save
		if c.link >= 0 {
			r.links[c.link] = outer
		}
		return lines
	}
	links, images, title := r.links, r.images, r.title
	*r = save
	r.links, r.images, r.title = links, images, title
	return lines
}

// gridRow emits one grid row: as many lines as its tallest cell, every cell
// padded to its width between the "│" borders.
func (r *renderer) gridRow(row gridRow, widths []int) {
	h := 1
	for _, cell := range row.cells {
		h = max(h, len(cell.lines))
	}
	if row.n != nil {
		if id, _ := attr(row.n, "id"); id != "" {
			r.anchor(id)
		}
	}
	for i := 0; i < h; i++ {
		body := []frag{r.border("│ ")}
		for j, cell := range row.cells {
			if j > 0 {
				body = append(body, r.border(" │ "))
			}
			cw := spanWidth(widths, cell)
			var fs []frag
			lw := 0
			if i < len(cell.lines) {
				fs, lw = cell.lines[i].frags, cell.lines[i].w
			}
			if lw > cw {
				fs, lw = cutFrags(fs, cw), cw
			}
			body = append(body, fs...)
			if lw < cw {
				body = append(body, frag{text: strings.Repeat(" ", cw-lw), w: cw - lw, link: -1, img: -1, off: -1})
			}
		}
		body = append(body, r.border(" │"))
		r.emit(r.fitGrid(body), lineSource(body, row.off))
	}
}

// rule is a horizontal border line between the rows whose cells are above
// and below (nil at the top and bottom edge); a junction shows where a
// column edge meets it. The rule under the header rows is drawn double.
func (r *renderer) rule(widths []int, above, below []gridCell, head bool) []frag {
	edges := func(cells []gridCell) []bool {
		if cells == nil {
			return nil
		}
		e := make([]bool, len(widths)+1)
		for _, cell := range cells {
			e[cell.col] = true
		}
		return e
	}
	up, down := edges(above), edges(below)
	// Glyphs by junction: left edge, right edge, cross, up only, down only,
	// none; the top and bottom edges pick their own corners below.
	g := [6]string{"├", "┤", "┼", "┴", "┬", "─"}
	if head {
		g = [6]string{"╞", "╡", "╪", "╧", "╤", "═"}
	}
	var b strings.Builder
	for k := 0; k <= len(widths); k++ {
		u, d := up != nil && up[k], down != nil && down[k]
		switch {
		case k == 0 && up == nil:
			b.WriteString("┌")
		case k == 0 && down == nil:
			b.WriteString("└")
		case k == 0:
			b.WriteString(g[0])
		case k == len(widths) && up == nil:
			b.WriteString("┐")
		case k == len(widths) && down == nil:
			b.WriteString("┘")
		case k == len(widths):
			b.WriteString(g[1])
		case u && d:
			b.WriteString(g[2])
		case u:
			b.WriteString(g[3])
		case d:
			b.WriteString(g[4])
		default:
			b.WriteString(g[5])
		}
		if k < len(widths) {
			b.WriteString(strings.Repeat(g[5], widths[k]+2))
		}
	}
	text := b.String()
	return r.fitGrid([]frag{{text: text, w: sum(widths) + 3*len(widths) + 1, st: r.sty.sep, link: -1, img: -1, off: -1}})
}

// border is a run of the grid's vertical border.
func (r *renderer) border(s string) frag {
	return frag{text: s, w: width(s), st: r.sty.sep, link: -1, img: -1, off: -1}
}

// fitGrid cuts a grid line wider than the pane at its right edge, marked
// with the hscroll overflow glyph like a long <pre> line.
func (r *renderer) fitGrid(line []frag) []frag {
	avail := r.avail()
	if fragsWidth(line) <= avail {
		return line
	}
	line = cutFrags(line, avail-1)
	return append(line, frag{text: hscroll.RightGlyph, w: 1, st: r.sty.edge, link: -1, img: -1, off: -1})
}

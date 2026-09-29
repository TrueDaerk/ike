package app

// playtable.go is the playground's table view (#2794): a result shaped like
// `[{id, name, …}, …]` — or a list of scalars, or a stream of either — drawn
// as a grid through the shared renderer (internal/gridview) in place of the
// result editor. One column per key (the union, in the CSV export's order),
// nested values as compact JSON, a missing key as the grid's ∅.
//
// The view is a per-playground toggle (playground.tableView, default
// ctrl+alt+l). Asking for it over a result that does not fit notifies and
// stays in the text view; once on, every new result re-checks its shape, and
// one that does not fit falls back to the text view with a notice on the info
// row — the toggle stays on, so the grid comes back with the next result that
// fits (typing through `.items` passes `.item`, which is null).
//
// The grid takes exactly the result editor's rectangle: the editor keeps its
// size (sizePlayResult), so the header rows above, the pane geometry and the
// mouse translation are the text view's; only what is drawn in the rectangle,
// and what a click or a key there means, changes.
//
// Inside it: a cell cursor (j/k rows, h/l columns), `s` or a header click
// sorts by a column (ascending, descending, off), `y` or the copy chord copies
// the cell, `Y` the row as JSON, enter drills into the row the way
// json.jqAppendPath does (`.[i]` appended to the program), and the find chord
// (or `/`) searches the cells with the shared in-pane search. tab, esc and the
// find chord's round trip from the query line behave as in the text view.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/itchyny/gojq"

	"ike/internal/docpath"
	"ike/internal/gridview"
	"ike/internal/host"
	"ike/internal/jqplay"
	"ike/internal/ui"
)

// TogglePlayTableMsg is playground.tableView: switch the result between the
// text view and the table view.
type TogglePlayTableMsg struct{}

// playTableMaxColW bounds a column's width in cells; longer values are cut
// with an ellipsis (y copies the whole value).
const playTableMaxColW = 40

// playTable is the grid of one installed result.
type playTable struct {
	grid jqplay.Grid
	// labels are the column names — the keys, or `value` for a list of
	// scalars — and widths their column widths, sized once per result
	// against the labels (sort marker included) and every cell.
	labels []string
	widths []int
	// order maps a display row to its grid row: the sort's permutation, the
	// identity while unsorted. sortCol is the sorted column, -1 for none.
	order    []int
	sortCol  int
	sortDesc bool
	// cur and col are the cell cursor (display row, column); top is the
	// first display row on screen, colOff the first column.
	cur, col    int
	top, colOff int
	// search is the cell search; its positions are display row × column
	// count + column, so ascending positions read the grid row by row.
	search ui.LineSearch
}

// newPlayTable builds the table of grid g.
func newPlayTable(g jqplay.Grid) *playTable {
	t := &playTable{grid: g, sortCol: -1, search: ui.LineSearch{Cur: -1}}
	t.labels = g.Columns
	if t.labels == nil {
		t.labels = []string{"value"}
	}
	t.order = make([]int, len(g.Rows))
	for i := range t.order {
		t.order[i] = i
	}
	t.sizeColumns()
	return t
}

// cols is the column count.
func (t *playTable) cols() int { return len(t.labels) }

// cell is the cell at display row r, column c.
func (t *playTable) cell(r, c int) jqplay.GridCell { return t.grid.Rows[t.order[r]][c] }

// headerLabels are the header cells: each column name with the sort marker on
// the sorted column, inside that column's width.
func (t *playTable) headerLabels() []string {
	out := make([]string, len(t.labels))
	for i, l := range t.labels {
		out[i] = l
		if i == t.sortCol {
			if t.sortDesc {
				out[i] += " ▼"
			} else {
				out[i] += " ▲"
			}
		}
	}
	return out
}

// sizeColumns sizes every column to its widest label (with room for a sort
// marker) or cell, clamped to playTableMaxColW.
func (t *playTable) sizeColumns() {
	t.widths = make([]int, len(t.labels))
	for i, l := range t.labels {
		t.widths[i] = ansi.StringWidth(l) + 2
	}
	for _, row := range t.grid.Rows {
		for i, c := range row {
			text := c.Text
			if c.Missing {
				text = gridview.NullCell
			}
			if w := ansi.StringWidth(text); w > t.widths[i] {
				t.widths[i] = w
			}
		}
	}
	for i := range t.widths {
		t.widths[i] = min(t.widths[i], playTableMaxColW)
	}
}

// sortBy cycles column c's sort — ascending, descending, off — keeping the
// cursor on the row it was on. Sorting another column starts it ascending.
func (t *playTable) sortBy(c int) {
	if c < 0 || c >= t.cols() {
		return
	}
	switch {
	case t.sortCol != c:
		t.sortCol, t.sortDesc = c, false
	case !t.sortDesc:
		t.sortDesc = true
	default:
		t.sortCol = -1
	}
	row := -1
	if t.cur < len(t.order) {
		row = t.order[t.cur]
	}
	t.applySort()
	for i, r := range t.order {
		if r == row {
			t.cur = i
			break
		}
	}
	t.recomputeSearch()
}

// applySort rebuilds order from the sort state. The sort is stable, so equal
// cells keep the result's order in both directions.
func (t *playTable) applySort() {
	for i := range t.order {
		t.order[i] = i
	}
	if t.sortCol < 0 {
		return
	}
	c, desc := t.sortCol, t.sortDesc
	sort.SliceStable(t.order, func(i, j int) bool {
		cmp := jqplay.CompareCells(t.grid.Rows[t.order[i]][c], t.grid.Rows[t.order[j]][c])
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

// recomputeSearch re-matches the query against every cell in display order.
func (t *playTable) recomputeSearch() {
	n := t.cols()
	t.search.Recompute(len(t.order)*n, func(p int) bool {
		c := t.cell(p/n, p%n)
		return !c.Missing && ui.SmartCaseContains(t.search.Text, c.Text)
	})
}

// gotoMatch puts the cell cursor on the current search match.
func (t *playTable) gotoMatch() {
	if p, ok := t.search.Current(); ok {
		t.cur, t.col = p/t.cols(), p%t.cols()
	}
}

// isMatch reports whether display cell (r, c) is a search hit.
func (t *playTable) isMatch(r, c int) bool {
	if t.search.Text == "" {
		return false
	}
	p := r*t.cols() + c
	i := sort.SearchInts(t.search.Matches, p)
	return i < len(t.search.Matches) && t.search.Matches[i] == p
}

// dataRows is the number of data rows a height-row grid shows: the
// header takes one row and an active search its prompt row.
func (t *playTable) dataRows(height int) int {
	rows := height - 1
	if t.search.Active() {
		rows--
	}
	return max(rows, 1)
}

// clamp keeps the cursor on a cell and the windows around it: rows through
// the shared list window, columns by scrolling colOff until the cursor column
// fits the width.
func (t *playTable) clamp(width, height int) {
	ui.ClampWindow(&t.cur, &t.top, len(t.order), t.dataRows(height))
	t.col = ui.ClampIndex(t.col, t.cols())
	if t.colOff > t.col {
		t.colOff = t.col
	}
	for t.colOff < t.col && !t.colFits(t.col, width) {
		t.colOff++
	}
}

// colFits reports whether column c ends inside width when the grid starts at
// colOff: a leading space, then each column and its two-space gap.
func (t *playTable) colFits(c, width int) bool {
	x := 1
	for i := t.colOff; i <= c; i++ {
		x += t.widths[i]
		if i < c {
			x += 2
		}
	}
	return x <= width
}

// colAt is the column under content-local x, false in a gap or past the end.
func (t *playTable) colAt(x int) (int, bool) {
	pos := 1
	for c := t.colOff; c < t.cols(); c++ {
		if x >= pos && x < pos+t.widths[c] {
			return c, true
		}
		pos += t.widths[c] + 2
	}
	return 0, false
}

// view renders the grid in a width × height rectangle: the header, the data
// rows, and the search prompt at the bottom while a search is active.
// focused paints the cursor row and cell as the keyboard's.
func (t *playTable) view(m Model, width, height int, focused bool) string {
	pal := m.pal()
	t.clamp(width, height)
	lines := make([]string, 0, height)
	lines = append(lines, gridview.HeaderRow(pal, t.headerLabels(), t.widths, t.colOff, width))
	rows := t.dataRows(height)
	cells := make([]gridview.Cell, t.cols())
	for k := 0; k < rows && len(lines) < height; k++ {
		r := t.top + k
		if r >= len(t.order) {
			lines = append(lines, "")
			continue
		}
		for c := range cells {
			gc := t.cell(r, c)
			cells[c] = gridview.Cell{Text: gc.Text, Null: gc.Missing, Cursor: r == t.cur && c == t.col, Match: t.isMatch(r, c)}
		}
		lines = append(lines, gridview.DataRow(pal, cells, t.widths, t.colOff, width, r == t.cur, focused))
	}
	if t.search.Active() && len(lines) < height {
		miss := lipgloss.NewStyle().Foreground(pal.Error)
		lines = append(lines, ansi.Truncate(" "+t.search.LineStyled(lipgloss.NewStyle().Faint(true), miss), width, "…"))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// counter is the info row's `row i/n` for the cursor.
func (t *playTable) counter() string {
	return "row " + strconv.Itoa(t.cur+1) + "/" + strconv.Itoa(len(t.order))
}

// syncPlayTable rebuilds the table for a freshly installed result while the
// view is on, and reports the notice to show when the result does not fit and
// the view falls back to the text. The sort survives a result that still has
// its column, so refining a query does not reset the order the user chose.
func (s *playState) syncPlayTable() (notice string) {
	if !s.tableOn {
		s.table = nil
		return ""
	}
	g, err := s.result.Grid()
	if err != nil {
		shown := s.table != nil
		s.table = nil
		if shown && s.haveResult {
			return "table view: " + err.Error() + " — showing text"
		}
		return ""
	}
	prev := s.table
	s.table = newPlayTable(g)
	if s.stripFocus {
		s.setBufFocus(true) // the strip hides under the grid; the grid takes its keys
	}
	if prev != nil && prev.sortCol >= 0 {
		name := prev.labels[prev.sortCol]
		for i, l := range s.table.labels {
			if l == name {
				s.table.sortCol, s.table.sortDesc = i, prev.sortDesc
				s.table.applySort()
				break
			}
		}
	}
	if prev != nil && prev.search.Text != "" {
		s.table.search.Field = prev.search.Field
		s.table.recomputeSearch()
	}
	return ""
}

// togglePlayTable is playground.tableView: the table view on over a result
// that fits, off when it is on. A result that does not fit notifies and stays
// in the text view.
func (m *Model) togglePlayTable() {
	s := m.play
	if s == nil {
		return
	}
	if s.tableOn {
		s.tableOn, s.table = false, nil
		s.status, s.statusWarn = "table view off — showing text", false
		m.sizePlayResult()
		return
	}
	reason := "the result is empty"
	if s.haveResult {
		g, err := s.result.Grid()
		if err == nil {
			if s.stripFocus {
				s.setBufFocus(true) // the strip hides under the grid; the grid takes its keys
			}
			s.tableOn, s.table = true, newPlayTable(g)
			s.status, s.statusWarn = fmt.Sprintf("table view: %d row(s) × %d column(s)", len(g.Rows), s.table.cols()), false
			m.sizePlayResult()
			return
		}
		reason = err.Error()
	}
	s.status, s.statusWarn = "table view: "+reason, true
	m.host.Notify(host.Info, "table view: "+reason)
}

// playTableFocused reports whether the table holds the keyboard.
func (s *playState) playTableFocused() bool {
	return s != nil && s.table != nil && s.bufFocus && !s.stripFocus
}

// playTableSize is the table's rectangle — the result editor's.
func (m Model) playTableSize() (width, height int) {
	ed := m.play.resultEd
	return ed.Width(), ed.Height()
}

// updatePlayTableKey routes a key while the table has the keyboard. It mirrors
// updatePlayBufferKey: the same result actions (ctrl+y / ctrl+o / ctrl+g /
// ctrl+l), tab back to the query line, esc out of a search first and then out
// of the mode, and modified chords resolved against the Global scope.
func (m Model) updatePlayTableKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.play
	t := s.table
	width, height := m.playTableSize()
	defer t.clamp(width, height)
	if t.search.Open {
		if m.playCopyChord(msg) {
			m.copyPlayTableCell()
			return m, nil
		}
		_, changed, action := t.search.Key(msg)
		switch {
		case changed:
			t.recomputeSearch()
			t.gotoMatch()
		case action == ui.SearchAccept:
			t.gotoMatch()
		case action == ui.SearchCancel && s.findQuery:
			// The find chord's round trip (#2411): a search opened from the
			// query line hands the keyboard back there.
			s.setBufFocus(false)
		}
		return m, nil
	}
	if m.playCopyChord(msg) {
		m.copyPlayTableCell()
		return m, nil
	}
	if m.playFindChord(msg) {
		return m.beginPlayResultSearch()
	}
	if delta, ok := m.playMatchStepChord(msg); ok {
		if out, cmd, handled := m.stepPlayResultSearch(delta); handled {
			return out, cmd
		}
	}
	key := msg.String()
	if ui.ListNav(key, &t.cur, len(t.order), t.dataRows(height), ui.NavFull) {
		return m, nil
	}
	switch key {
	case "h", "left":
		t.col = max(t.col-1, 0)
		return m, nil
	case "l", "right":
		t.col = min(t.col+1, t.cols()-1)
		return m, nil
	case "0", "^":
		t.col = 0
		return m, nil
	case "$":
		t.col = t.cols() - 1
		return m, nil
	case "s":
		t.sortBy(t.col)
		return m, nil
	case "y":
		m.copyPlayTableCell()
		return m, nil
	case "Y":
		m.copyPlayTableRow()
		return m, nil
	case "/":
		return m.beginPlayResultSearch()
	case "n", "N":
		delta := 1
		if key == "N" {
			delta = -1
		}
		out, cmd, _ := m.stepPlayResultSearch(delta)
		return out, cmd
	case "enter":
		return m, m.drillPlayTableRow()
	case "tab":
		s.setBufFocus(false)
		return m, nil
	case "ctrl+y":
		m.copyPlayResult()
		return m, nil
	case "ctrl+o":
		return m.openPlayResultAsScratch()
	case "ctrl+g":
		m.openPlayCheatsheet(s.dialect, "")
		return m, nil
	case "ctrl+l":
		return m, m.clearPlayResult()
	case "esc":
		if s.findQuery {
			s.setBufFocus(false)
			return m, nil
		}
		if t.search.Text != "" {
			// vim's :noh — the committed search goes before the mode does.
			t.search.Reset()
			return m, nil
		}
		if cmd, ok := m.leavePlayStepping(); ok {
			return m, cmd
		}
		m.leavePlaygroundOnEsc()
		return m, nil
	}
	if m.playCodeActionChord(msg) {
		m.playNoCodeActions()
		return m, nil
	}
	if handled, cmd := m.playGlobalChord(msg); handled {
		return m, cmd
	}
	m.recordPlayUnbound(msg)
	return m, nil
}

// beginPlayTableSearch opens the cell search with the keyboard in the table,
// the table's side of beginPlayResultSearch.
func (s *playState) beginPlayTableSearch(fromQuery bool) {
	s.setBufFocus(true)
	s.findQuery = fromQuery
	s.table.search.Start()
}

// stepPlayTableSearch steps the committed cell search by delta, the table's
// side of stepPlayResultSearch; false with no query.
func (m Model) stepPlayTableSearch(delta int) bool {
	t := m.play.table
	if t.search.Text == "" {
		return false
	}
	st := t.search.Step(delta)
	t.gotoMatch()
	t.clamp(m.playTableSize())
	s := m.play
	s.status, s.statusWarn = "/"+t.search.Text+"  "+t.search.Counter(), st.Total == 0
	return true
}

// copyPlayTableCell copies the cursor cell: a string's own text, any other
// value as its JSON.
func (m *Model) copyPlayTableCell() {
	s := m.play
	t := s.table
	if len(t.order) == 0 {
		return
	}
	c := t.cell(t.cur, t.col)
	if c.Missing {
		s.status, s.statusWarn = "nothing to copy — row "+strconv.Itoa(t.cur+1)+" has no "+t.labels[t.col], true
		return
	}
	text, ok := c.Value.(string)
	if !ok {
		text = playJSON(c.Value)
	}
	m.copyToClipboard(text)
	s.status, s.statusWarn = "copied the "+t.labels[t.col]+" cell", false
}

// copyPlayTableRow copies the cursor row's value as compact JSON.
func (m *Model) copyPlayTableRow() {
	s := m.play
	t := s.table
	if len(t.order) == 0 {
		return
	}
	row := t.grid.Rows[t.order[t.cur]]
	var v any
	if t.grid.Columns == nil {
		v = row[0].Value
	} else {
		obj := make(map[string]any, len(row))
		for i, c := range row {
			if !c.Missing {
				obj[t.labels[i]] = c.Value
			}
		}
		v = obj
	}
	m.copyToClipboard(playJSON(v))
	s.status, s.statusWarn = "copied row "+strconv.Itoa(t.cur+1)+" as JSON", false
}

// playJSON is a value's compact JSON, the way jq -c prints it.
func playJSON(v any) string {
	b, err := gojq.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// drillPlayTableRow is enter on a row: its index appended to the program as a
// path stage, exactly as json.jqAppendPath appends the result cursor's path —
// the row's position in the result, whatever the sort shows it at.
func (m *Model) drillPlayTableRow() tea.Cmd {
	s := m.play
	t := s.table
	if len(t.order) == 0 {
		return nil
	}
	if t.grid.Stream {
		s.status, s.statusWarn = "the rows are a stream of values, not one array — wrap the program in [ … ] to drill into a row", true
		return nil
	}
	steps := []docpath.Step{{Seq: true, Index: t.order[t.cur]}}
	path, reason := playResultPath(s.dialect, s.result, true, steps, false)
	if reason != "" {
		s.status, s.statusWarn = reason, true
		return nil
	}
	return m.appendPlayStage(path)
}

// clickPlayTable handles a left press at content-local x/y while the table
// is shown, reporting whether it landed on it: the header sorts the clicked
// column, a data row moves the cell cursor there and focuses the table.
func (m *Model) clickPlayTable(x, y int) bool {
	s := m.play
	t := s.table
	if t == nil || y < 0 {
		return false
	}
	width, height := m.playTableSize()
	c, onCol := t.colAt(x)
	if y == 0 {
		if onCol {
			t.sortBy(c)
		}
		s.setBufFocus(true)
		t.clamp(width, height)
		return true
	}
	if r, ok := ui.RowAt(y, t.top, 1, t.dataRows(height), len(t.order)); ok {
		t.cur = r
		if onCol {
			t.col = c
		}
	}
	s.setBufFocus(true)
	t.clamp(width, height)
	return true
}

// wheelPlayTable scrolls the table by dy rows and dx columns, dragging the
// cursor along so it stays on screen.
func (m *Model) wheelPlayTable(dx, dy int) {
	t := m.play.table
	width, height := m.playTableSize()
	if dy != 0 {
		ui.WheelWindow(&t.top, &t.cur, dy, len(t.order), t.dataRows(height))
	}
	if dx != 0 {
		t.colOff = ui.ClampIndex(t.colOff+dx, t.cols())
		t.col = t.colOff
	}
	t.clamp(width, height)
}

// playTableView is the table in the result's rectangle.
func (m Model) playTableView() string {
	s := m.play
	width, height := m.playTableSize()
	return s.table.view(m, width, height, m.playFocused() && s.playTableFocused())
}

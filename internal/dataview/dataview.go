// Package dataview is the data viewer pane (#1764): a sidebar listing a
// database's tables and views, and a paged read-only grid of the selected
// table's rows. The pane speaks only internal/datasrc's Source interface —
// nothing here is SQLite-specific, so the DuckDB (#1765) and Parquet (#1766)
// viewers reuse it with their own backends.
//
// Rows are fetched one page (PageSize) at a time via the backend; the full
// table is never loaded into memory, and the backend contract keeps the
// underlying file untouched.
package dataview

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/datasrc"
	"ike/internal/highlight"
	"ike/internal/theme"
	"ike/internal/ui"
)

// PageSize is how many rows one page fetch asks the backend for.
const PageSize = 500

// maxColWidth clamps one grid column; longer cells clip with an ellipsis.
const maxColWidth = 32

// nullCell is how the grid draws SQL NULL — visibly distinct from the empty
// string, which renders as nothing.
const nullCell = "∅"

// ShowSchemaMsg asks the root model to show a table's DDL in a read-only
// buffer. Emitted by the schema key on the selected table.
type ShowSchemaMsg struct {
	// Path is the database file, Table the object whose DDL SQL holds.
	Path  string
	Table string
	SQL   string
}

// region is which half of the pane has the cursor.
type region int

const (
	regionSidebar region = iota
	regionGrid
)

// Model is one data viewer pane bound to a database file.
type Model struct {
	key  string
	path string
	pal  *theme.Palette

	src    datasrc.Source
	tables []datasrc.Table
	err    error // open/listing error: the pane renders it as its notice

	// Async open and lazy counts (#1795), all driven from async.go: started
	// guards the one-shot Init, opening drives the loading notice, closed
	// tells a landing open result that its pane is gone. totals caches one
	// row count per table and filter, totalExact says whether the loaded
	// page's total is a counted number or the engine's estimate, and counting
	// keeps at most one count in flight.
	started    bool
	opening    bool
	closed     bool
	totals     map[totalKey]totalVal
	totalExact bool
	counting   bool

	region region
	tcur   int // sidebar cursor
	ttop   int // sidebar scroll

	sel     int // index of the loaded table, -1 before the first load
	page    datasrc.Page
	pageErr error
	rowCur  int // cursor within the loaded page
	rowTop  int // vertical scroll within the loaded page
	colOff  int // first visible column (horizontal scroll)

	// Filter state (#1777): filter is the applied clause ("" — unfiltered),
	// the f* fields the open filter line. hl styles it as SQL.
	filter   string
	fEditing bool
	fInput   string
	fCur     int
	fErr     error
	hl       highlight.Theme

	// Mouse state (#1788): loading a table by mouse needs a second click on
	// the same sidebar row within doubleClickWindow; now is swappable in
	// tests.
	lastClickRow int
	lastClickAt  time.Time
	now          func() time.Time

	w, h    int
	focused bool
}

// New builds the pane for the database at p. It touches no file: the engine
// open, the table listing and the first page all run in the background command
// Init returns (#1795), so a pane over a multi-gigabyte database appears in
// the same frame as one over an empty file. Until the open lands the pane
// draws its loading notice; an open error lands the same way and degrades to a
// notice, so an encrypted or corrupt file explains itself.
func New(key, p string, pal *theme.Palette) Model {
	m := Model{key: key, path: p, pal: pal, sel: -1, lastClickRow: -1, now: time.Now}
	m.rebuildTheme()
	return m
}

// Key returns the model's own key, which async results are routed by — the
// owning pane may be re-keyed or moved into a tab (#1778).
func (m *Model) Key() string { return m.key }

// Path returns the database file's path.
func (m *Model) Path() string { return m.path }

// Err returns the open/listing error, if any.
func (m *Model) Err() error { return m.err }

// Close releases the backend; called when the pane closes. A still-running
// open is remembered as closed, so the engine it hands over is released rather
// than leaked (#1795).
func (m *Model) Close() {
	m.closed = true
	if m.src != nil {
		m.src.Close()
		m.src = nil
	}
}

// Tables reports the number of listed tables and views (tests).
func (m *Model) Tables() int { return len(m.tables) }

// SelectedTable returns the loaded table's name, "" before the first load
// (tests).
func (m *Model) SelectedTable() string {
	if m.sel < 0 || m.sel >= len(m.tables) {
		return ""
	}
	return m.tables[m.sel].Name
}

// PageOffset returns the loaded page's first-row index (tests).
func (m *Model) PageOffset() int64 { return m.page.Offset }

// PageRows reports the loaded page's row count (tests).
func (m *Model) PageRows() int { return len(m.page.Rows) }

// Cursor reports the grid's row cursor within the page (tests).
func (m *Model) Cursor() int { return m.rowCur }

// InGrid reports whether the grid half holds the region focus rather than the
// sidebar (tests).
func (m *Model) InGrid() bool { return m.region == regionGrid }

// ColumnOffset reports the first visible grid column (tests).
func (m *Model) ColumnOffset() int { return m.colOff }

// SidebarWidth is the table list's width in cells — the x at which the grid
// starts, which the mouse routing's tests need.
func (m *Model) SidebarWidth() int { return m.sidebarWidth() }

// SetSize records the pane interior in cells.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h; m.clampScroll() }

// SetFocused records focus for the header and the selection highlight.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the theme palette.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p; m.rebuildTheme() }

// loadTable selects table i and fetches its first page. The filter is dropped
// with the table it was written for — a clause naming another table's columns
// would only fail.
func (m *Model) loadTable(i int) {
	if i < 0 || i >= len(m.tables) {
		return
	}
	m.sel, m.tcur = i, i
	m.rowCur, m.rowTop, m.colOff = 0, 0, 0
	m.filter, m.fInput, m.fCur, m.fErr = "", "", 0, nil
	m.fetch(0)
}

// fetch loads one page of the selected table starting at offset — of the
// filtered result while a filter is applied, so paging keeps its window on
// what the grid actually shows.
func (m *Model) fetch(offset int64) {
	if m.src == nil || m.sel < 0 {
		return
	}
	if m.filter != "" {
		m.page, m.pageErr = m.src.PageWhere(m.tables[m.sel].Name, m.filter, offset, PageSize)
	} else {
		m.page, m.pageErr = m.src.Page(m.tables[m.sel].Name, offset, PageSize)
	}
	if m.pageErr != nil {
		m.page = datasrc.Page{Offset: offset}
	}
	// The total never comes from the fetch (#1795): a backend that knows one
	// for free hands it over, everything else reads the count cache — which is
	// why walking a table with n/p issues no COUNT(*) at all.
	m.recordPageTotal()
	m.adoptTotal()
	if m.rowCur >= len(m.page.Rows) {
		m.rowCur = len(m.page.Rows) - 1
	}
	if m.rowCur < 0 {
		m.rowCur = 0
	}
	m.clampScroll()
}

// Update handles one message: key presses, focus-filtered by the pane layer,
// and the background results of the open and the row counts (#1795), which the
// root model routes here by pane key.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case ResultMsg:
		return m.applyResult(msg)
	}
	return nil
}

// handleKey runs one key and returns its command, batched with the count the
// new state may need: switching table or filter makes a different result set
// the one worth counting, and countCmd is the single place that decides.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.err != nil || m.src == nil {
		return nil
	}
	return tea.Batch(m.keyAction(msg), m.countCmd())
}

func (m *Model) keyAction(msg tea.KeyPressMsg) tea.Cmd {
	// The filter line owns the keyboard while it is open (#1777): its text is
	// SQL, so the grid's single-letter keys must not fire mid-clause.
	if m.fEditing {
		m.filterKey(msg)
		return nil
	}
	switch msg.String() {
	case "tab":
		if m.region == regionSidebar {
			m.region = regionGrid
		} else {
			m.region = regionSidebar
		}
		return nil
	case "s":
		return m.showSchema()
	}
	if m.region == regionSidebar {
		m.sidebarKey(msg)
		return nil
	}
	m.gridKey(msg)
	return nil
}

// sidebarKey navigates the table list; enter/l loads the selection and moves
// to the grid.
func (m *Model) sidebarKey(msg tea.KeyPressMsg) {
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(msg.String(), &m.tcur, len(m.tables), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return
	}
	switch msg.String() {
	case "enter", "l", "right":
		if m.tcur != m.sel {
			m.loadTable(m.tcur)
		}
		m.region = regionGrid
	}
}

// gridKey navigates the loaded page: j/k rows (crossing a page edge fetches
// the neighbour page), h/l columns, pgup/pgdown a screenful, n/p (ctrl+f /
// ctrl+b) whole DB pages, g/G first/last page.
func (m *Model) gridKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "j", "down":
		m.rowStep(1)
	case "k", "up":
		m.rowStep(-1)
	case "h", "left":
		if m.colOff > 0 {
			m.colOff--
		} else {
			// At the left edge h falls through to the sidebar, the pane's
			// spatial layout.
			m.region = regionSidebar
		}
	case "l", "right":
		if m.colOff < len(m.page.Columns)-1 {
			m.colOff++
		}
	case "n", "ctrl+f":
		m.pageStep(1)
	case "p", "ctrl+b":
		m.pageStep(-1)
	case "pgdown":
		m.screenStep(1)
	case "pgup":
		m.screenStep(-1)
	case "g", "home":
		m.fetch(0)
		m.rowCur = 0
	case "G", "end":
		m.lastPage()
	case "/":
		m.startFilter()
	}
	m.clampScroll()
}

// rowStep moves the row cursor, fetching the adjacent page when the cursor
// walks off the loaded one and more rows exist on that side.
func (m *Model) rowStep(d int) {
	next := m.rowCur + d
	if next >= 0 && next < len(m.page.Rows) {
		m.rowCur = next
		return
	}
	if d > 0 && m.hasNextPage() {
		m.fetch(m.page.Offset + PageSize)
		m.rowCur, m.rowTop = 0, 0
	} else if d < 0 && m.page.Offset > 0 {
		m.fetch(max64(0, m.page.Offset-PageSize))
		m.rowCur = len(m.page.Rows) - 1
	}
}

// screenStep pages the row cursor by one screenful — what pgup/pgdown mean
// everywhere else (#1788). It moves *within* the loaded page; a table shorter
// than PageSize therefore still scrolls, which the whole-page fetch below
// could not do. Only a cursor already sitting on the page's edge crosses it,
// fetching the neighbour page like rowStep does.
func (m *Model) screenStep(d int) {
	h := m.gridHeight()
	if d > 0 {
		if next := m.rowCur + h; next < len(m.page.Rows) {
			m.rowCur = next
			return
		}
		if m.rowCur < len(m.page.Rows)-1 {
			m.rowCur = len(m.page.Rows) - 1
			return
		}
		if m.hasNextPage() {
			m.fetch(m.page.Offset + PageSize)
			m.rowCur, m.rowTop = 0, 0
		}
		return
	}
	if next := m.rowCur - h; next >= 0 {
		m.rowCur = next
		return
	}
	if m.rowCur > 0 {
		m.rowCur = 0
		return
	}
	if m.page.Offset > 0 {
		m.fetch(max64(0, m.page.Offset-PageSize))
		m.rowCur = len(m.page.Rows) - 1
	}
}

// pageStep fetches the next or previous page wholesale.
func (m *Model) pageStep(d int) {
	if d > 0 && m.hasNextPage() {
		m.fetch(m.page.Offset + PageSize)
		m.rowCur, m.rowTop = 0, 0
	} else if d < 0 && m.page.Offset > 0 {
		m.fetch(max64(0, m.page.Offset-PageSize))
		m.rowCur, m.rowTop = 0, 0
	}
}

// lastPage jumps to the final page. It needs an exact total: an estimate
// (#1795) would land the jump next to the end — past it on a table with
// deleted rows — so G stays inert until the background count has landed.
func (m *Model) lastPage() {
	if !m.totalExact || m.page.Total < 0 {
		return
	}
	last := (max64(m.page.Total-1, 0) / PageSize) * PageSize
	m.fetch(last)
	m.rowCur = len(m.page.Rows) - 1
}

// hasNextPage reports whether rows exist past the loaded page. Only a counted
// total decides it; with an estimate, or none at all, a full page suggests
// more — which is what keeps paging working before the count lands (#1795).
func (m *Model) hasNextPage() bool {
	if m.totalExact && m.page.Total >= 0 {
		return m.page.Offset+int64(len(m.page.Rows)) < m.page.Total
	}
	return len(m.page.Rows) == PageSize
}

// showSchema fetches the selected table's DDL and asks the root model to
// show it read-only.
func (m *Model) showSchema() tea.Cmd {
	i := m.tcur
	if m.region == regionGrid {
		i = m.sel
	}
	if m.src == nil || i < 0 || i >= len(m.tables) {
		return nil
	}
	name := m.tables[i].Name
	ddl, err := m.src.Schema(name)
	if err != nil {
		return nil
	}
	msg := ShowSchemaMsg{Path: m.path, Table: name, SQL: ddl}
	return func() tea.Msg { return msg }
}

// View renders the header, the sidebar+grid body, and the status footer.
func (m *Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	pal := m.theme()
	if m.err != nil {
		return m.errorView(pal)
	}
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	b.WriteString(m.body(pal))
	if m.fEditing {
		b.WriteString(m.filterLine(pal))
		b.WriteString("\n")
	}
	b.WriteString(m.footer(pal))
	return b.String()
}

// errorView is the whole-pane notice for a database that would not open —
// encrypted, corrupt, locked by a writer, or unreadable. A missing engine
// tool is actionable and gets the dialog instead.
func (m *Model) errorView(pal *theme.Palette) string {
	var missing *datasrc.MissingToolError
	if errors.As(m.err, &missing) {
		return m.missingToolView(pal, missing)
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(pal.Foreground).Render(path.Base(m.path))
	note := lipgloss.NewStyle().Foreground(pal.Error).Render("cannot open database: " + m.err.Error())
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, title+"\n"+note)
}

// missingToolView is the centered dialog for a backend whose external tool is
// not installed (the DuckDB engine's `duckdb` CLI, #1765). The state is
// actionable — the user can install the tool and reopen the file — so it gets
// a prominent box with the install hints rather than a one-line notice. A
// pane too small for the box falls back to the plain notice.
func (m *Model) missingToolView(pal *theme.Palette, e *datasrc.MissingToolError) string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(pal.Error).Render(e.Tool + " not found"),
		"",
		lipgloss.NewStyle().Foreground(pal.Foreground).Render(path.Base(m.path) + " needs it: " + e.Why),
	}
	for _, hint := range e.Hints {
		lines = append(lines, lipgloss.NewStyle().Foreground(pal.Accent).Render("  "+hint))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Padding(0, 2).
		Render(strings.Join(lines, "\n"))
	if lipgloss.Width(box) > m.w || lipgloss.Height(box) > m.h {
		note := lipgloss.NewStyle().Foreground(pal.Error).Render(e.Error())
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, note)
	}
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

// headerLine names the database, its object count and the active filter.
func (m *Model) headerLine(pal *theme.Palette) string {
	n := len(m.tables)
	counts := fmt.Sprintf("%d object%s", n, plural(n))
	if m.opening {
		counts = "opening…" // nothing is listed yet (#1795)
	}
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" " + path.Base(m.path))
	line := title + lipgloss.NewStyle().Faint(true).Render("   "+counts)
	return line + m.filterNote(pal, m.w-lipgloss.Width(line))
}

// body joins the sidebar and the grid side by side.
func (m *Model) body(pal *theme.Palette) string {
	h := m.bodyHeight()
	side := m.renderSidebar(pal, h)
	grid := m.renderGrid(pal, h)
	return lipgloss.JoinHorizontal(lipgloss.Top, side, grid) + "\n"
}

// sidebarWidth is the table list's column width.
func (m *Model) sidebarWidth() int {
	w := m.w / 3
	if w > 28 {
		w = 28
	}
	if w < 12 {
		w = 12
	}
	return w
}

// renderSidebar draws the table/view list with row counts.
func (m *Model) renderSidebar(pal *theme.Palette, height int) string {
	w := m.sidebarWidth()
	var b strings.Builder
	for k := 0; k < height; k++ {
		line := strings.Repeat(" ", w)
		if i := m.ttop + k; i < len(m.tables) {
			line = m.sidebarRow(pal, i, w)
		}
		b.WriteString(line)
		if k < height-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// countText renders a row count for the sidebar and the status line: a
// counted number plain, an estimate marked with "~" so a guess never reads as
// a fact (#1795), and an unknown count as "?".
func countText(n int64, exact bool) string {
	switch {
	case n < 0:
		return "?"
	case exact:
		return fmt.Sprintf("%d", n)
	default:
		return fmt.Sprintf("~%d", n)
	}
}

// sidebarRow draws one object: marker, name, and right-aligned row count
// ("?" while the count is unknown, "~n" for an estimate, "view" tag for
// views).
func (m *Model) sidebarRow(pal *theme.Palette, i, w int) string {
	t := m.tables[i]
	count := countText(t.Rows, !t.Estimated)
	if t.IsView() {
		count += " v"
	}
	marker := "  "
	if i == m.sel {
		marker = "▸ "
	}
	left := " " + marker + t.Name
	pad := w - lipgloss.Width(left) - lipgloss.Width(count) - 1
	var line string
	if pad >= 1 {
		line = left + strings.Repeat(" ", pad) + count + " "
	} else {
		line = clipTo(left, w)
		if d := w - lipgloss.Width(line); d > 0 {
			line += strings.Repeat(" ", d)
		}
	}
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	if t.IsView() {
		style = style.Foreground(pal.Accent)
	}
	if i == m.tcur && m.region == regionSidebar {
		if m.focused {
			style = style.Background(pal.Selection).Bold(true)
		} else {
			style = style.Background(pal.SelectionMuted)
		}
	}
	return style.Render(line)
}

// renderGrid draws the loaded page: header row, then the scrolled data rows.
func (m *Model) renderGrid(pal *theme.Palette, height int) string {
	w := m.w - m.sidebarWidth() - 1
	if w < 4 {
		return ""
	}
	sep := lipgloss.NewStyle().Faint(true).Render("│")
	var b strings.Builder
	if m.pageErr != nil {
		b.WriteString(sep + lipgloss.NewStyle().Foreground(pal.Error).Render(clipTo(" "+m.pageErr.Error(), w)))
	} else if m.opening {
		// The open runs in the background (#1795): the pane is here from the
		// first frame and says what it is waiting for.
		b.WriteString(sep + lipgloss.NewStyle().Faint(true).Render(" opening "+path.Base(m.path)+"…"))
	} else if m.sel < 0 || len(m.page.Columns) == 0 {
		b.WriteString(sep + lipgloss.NewStyle().Faint(true).Render(" no table selected"))
	}
	if m.pageErr != nil || m.opening || m.sel < 0 || len(m.page.Columns) == 0 {
		for k := 1; k < height; k++ {
			b.WriteString("\n" + sep)
		}
		return b.String()
	}
	widths := m.colWidths(w)
	b.WriteString(sep + m.headerRow(pal, widths, w))
	rows := height - 1
	for k := 0; k < rows; k++ {
		b.WriteString("\n" + sep)
		if i := m.rowTop + k; i < len(m.page.Rows) {
			b.WriteString(m.dataRow(pal, i, widths, w))
		}
	}
	return b.String()
}

// colWidths sizes the visible columns from the header and the loaded page,
// clamped to maxColWidth.
func (m *Model) colWidths(w int) []int {
	widths := make([]int, len(m.page.Columns))
	for i, c := range m.page.Columns {
		widths[i] = lipgloss.Width(c)
	}
	for _, row := range m.page.Rows {
		for i, cell := range row {
			t := cell.Text
			if cell.Null {
				t = nullCell
			}
			if n := lipgloss.Width(t); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for i := range widths {
		if widths[i] > maxColWidth {
			widths[i] = maxColWidth
		}
		if widths[i] < 1 {
			widths[i] = 1
		}
	}
	return widths
}

// headerRow draws the column names from colOff on, bold.
func (m *Model) headerRow(pal *theme.Palette, widths []int, w int) string {
	var cells []string
	for i := m.colOff; i < len(m.page.Columns); i++ {
		cells = append(cells, padTo(m.page.Columns[i], widths[i]))
	}
	line := " " + strings.Join(cells, "  ")
	return lipgloss.NewStyle().Bold(true).Foreground(pal.Accent).Render(clipTo(line, w))
}

// dataRow draws one page row from colOff on; NULL cells render as the null
// glyph, faint, so they never read as empty strings.
func (m *Model) dataRow(pal *theme.Palette, i int, widths []int, w int) string {
	row := m.page.Rows[i]
	base := lipgloss.NewStyle().Foreground(pal.Foreground)
	nullStyle := lipgloss.NewStyle().Faint(true)
	selected := i == m.rowCur && m.region == regionGrid
	if selected {
		if m.focused {
			base = base.Background(pal.Selection).Bold(true)
			nullStyle = nullStyle.Background(pal.Selection)
		} else {
			base = base.Background(pal.SelectionMuted)
			nullStyle = nullStyle.Background(pal.SelectionMuted)
		}
	}
	budget := w
	var b strings.Builder
	b.WriteString(base.Render(" "))
	budget--
	for c := m.colOff; c < len(row) && budget > 0; c++ {
		cell := row[c]
		text, style := cell.Text, base
		if cell.Null {
			text, style = nullCell, nullStyle
		}
		chunk := padTo(text, widths[c])
		if c < len(row)-1 {
			chunk += "  "
		}
		chunk = clipTo(chunk, budget)
		b.WriteString(style.Render(chunk))
		budget -= lipgloss.Width(chunk)
	}
	if selected && budget > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", budget)))
	}
	return b.String()
}

// footer is the status line: cursor position within the table plus the key
// hints that fit. With the filter line open it belongs to the filter — the
// engine's rejection of a clause, or the two keys that close the line.
func (m *Model) footer(pal *theme.Palette) string {
	if m.fEditing {
		if m.fErr != nil {
			return lipgloss.NewStyle().Foreground(pal.Error).Render(clipTo(" "+m.fErr.Error(), m.w))
		}
		return lipgloss.NewStyle().Faint(true).Render(
			clipTo(" enter apply · esc drop the filter · the clause follows the dimmed prefix", m.w))
	}
	status := ""
	switch {
	case m.opening:
		status = "opening…"
	case m.sel >= 0 && m.pageErr == nil:
		n := len(m.page.Rows)
		if n == 0 {
			status = "empty table"
			if m.page.Total == 0 && m.totalExact {
				status = "0 rows"
			}
		} else {
			first := m.page.Offset + 1
			last := m.page.Offset + int64(n)
			// The total is the counted number, the engine's estimate ("~1200",
			// #1795) or "?" — the line never passes a guess off as a count.
			status = fmt.Sprintf("rows %d–%d of %s", first, last, countText(m.page.Total, m.totalExact))
		}
	}
	hints := "tab switch · enter open · j/k row · h/l column · pgup/pgdn screen · n/p page · / filter · s schema"
	line := " " + status
	if status != "" {
		line += " · "
	}
	line += hints
	return lipgloss.NewStyle().Faint(true).Render(clipTo(line, m.w))
}

// bodyHeight is the room between the header and footer lines — one row less
// while the filter line sits above the footer.
func (m *Model) bodyHeight() int {
	h := m.h - 2
	if m.fEditing {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

// gridHeight is how many data rows the grid shows: the body minus the line it
// spends on the column headers.
func (m *Model) gridHeight() int {
	if h := m.bodyHeight() - 1; h >= 1 {
		return h
	}
	return 1
}

// clampScroll keeps both cursors valid and inside their visible windows.
func (m *Model) clampScroll() {
	clamp(&m.tcur, len(m.tables))
	clamp(&m.rowCur, len(m.page.Rows))
	scrollTo(&m.ttop, m.tcur, m.bodyHeight())
	scrollTo(&m.rowTop, m.rowCur, m.gridHeight())
	if m.colOff < 0 {
		m.colOff = 0
	}
	if n := len(m.page.Columns); m.colOff >= n && n > 0 {
		m.colOff = n - 1
	}
}

// clamp bounds a cursor to [0, n).
func clamp(c *int, n int) {
	if *c > n-1 {
		*c = n - 1
	}
	if *c < 0 {
		*c = 0
	}
}

// scrollTo moves a window top so the cursor is inside a window of height h.
func scrollTo(top *int, cur, h int) {
	if *top > cur {
		*top = cur
	}
	if cur >= *top+h {
		*top = cur - h + 1
	}
	if *top < 0 {
		*top = 0
	}
}

// padTo right-pads (or ellipsis-clips) s to exactly width cells.
func padTo(s string, width int) string {
	if lipgloss.Width(s) > width {
		return clipTo(s, width)
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

// clipTo bounds one rendered chunk to width cells, ellipsis on overflow.
func clipTo(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	if width < 1 {
		return ""
	}
	// Runes approximate cells closely enough here: grid text is
	// backend-rendered plain strings.
	if len(r) > width {
		r = r[:width-1]
		return string(r) + "…"
	}
	return s
}

// plural is the object-count suffix.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// max64 is the int64 max.
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

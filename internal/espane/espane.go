// Package espane is the Elasticsearch console pane (#1927): a sidebar listing
// a configured cluster's indices and aliases with doc counts, and a paged
// read-only grid of search hits against the selected index. The pane speaks
// only internal/esq — no HTTP or raw JSON — and mirrors the data viewer's
// layout and keys (wiki/architecture/data-viewer.md), with one deliberate
// difference: every fetch is asynchronous. The data viewer pages synchronously
// because its engines are local files; here every page is a network round
// trip, so paging follows the open's background pattern instead — a slow or
// dead cluster degrades to a notice, never a stall.
//
// Queries live outside the pane: each index has a Query-DSL buffer (an
// ordinary JSON file, internal/esq/query.go) edited in the normal editor with
// mapping-aware completion; running it lands here as the grid's active query.
package espane

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/esq"
	"ike/internal/gridview"
	"ike/internal/theme"
	"ike/internal/ui"
)

// PageSize is how many hits one page fetch asks the cluster for. Smaller than
// the data viewer's 500: every page is a network fetch of full documents, and
// from/size paging deepens linearly in cost.
const PageSize = 100

// maxColWidth clamps one grid column; longer cells clip with an ellipsis.
const maxColWidth = 32

// ShowJSONMsg asks the root model to show a JSON document read-only: an index
// mapping, one hit, or a result's aggregations. Name becomes part of the
// virtual buffer path (`<endpoint>!<name>.json`).
type ShowJSONMsg struct {
	Endpoint string
	Name     string
	JSON     string
}

// OpenQueryMsg asks the root model to open the index's query buffer in an
// editor tab. The pane never touches the disk itself — allocating the file is
// the app's move (internal/esq.QueryPath).
type OpenQueryMsg struct {
	Endpoint string
	Index    string
}

// region is which half of the pane has the cursor.
type region int

const (
	regionSidebar region = iota
	regionGrid
)

// Model is one console pane bound to a configured endpoint by name. The
// endpoint is re-resolved against the live config on every open, so a config
// edit heals a restored pane.
type Model struct {
	key      string
	endpoint string
	pal      *theme.Palette

	client  *esq.Client
	indices []esq.Index
	err     error // open/listing error: the pane renders it as its notice

	// Async state: started guards the one-shot Init, opening drives the
	// connecting notice, closed tells a landing result its pane is gone.
	// loading marks a page fetch in flight (one at a time); fetchSeq stamps
	// fetches so a stale result — user switched index mid-flight — is dropped
	// rather than applied.
	started  bool
	opening  bool
	closed   bool
	loading  bool
	fetchSeq int
	// pendingBottom asks the next landing page to put the cursor on its last
	// row — paging backwards with k arrives at the previous page's bottom.
	pendingBottom bool

	region region
	icur   int // sidebar cursor
	itop   int // sidebar scroll

	sel     int // index of the loaded index, -1 before the first load
	res     *esq.Result
	pageErr error
	rowCur  int // cursor within the loaded page
	rowTop  int // vertical scroll within the loaded page
	colOff  int // first visible column (horizontal scroll)

	// queries holds each index's active Query-DSL body ("" — match all);
	// mappings/fieldsOf cache what the background mapping fetch brought back,
	// keyed by index name.
	queries  map[string]string
	mappings map[string]string
	// pending is a query queued before the cluster listing landed (the
	// run-from-editor path may open the pane first); applyOpen consumes it.
	pending *pendingQuery

	// Double-click detection (#2259) mirrors the data viewer: loading an index
	// with the mouse needs a second click on it within ui.DoubleClickWindow;
	// now is injectable so tests control the clock.
	clicks ui.ClickTracker
	now    func() time.Time

	w, h    int
	focused bool
}

// New builds the pane for the named endpoint. It touches no network: the
// connect, the index listing and the first page all run in the background
// command Init returns, so the pane appears instantly and a dead cluster
// explains itself in a notice.
func New(key, endpoint string, pal *theme.Palette) Model {
	return Model{
		key: key, endpoint: endpoint, pal: pal, sel: -1,
		now:      time.Now,
		queries:  map[string]string{},
		mappings: map[string]string{},
	}
}

// Key returns the model's own key, which async results are routed by.
func (m *Model) Key() string { return m.key }

// Endpoint returns the configured endpoint name the pane is bound to.
func (m *Model) Endpoint() string { return m.endpoint }

// Err returns the open/listing error, if any.
func (m *Model) Err() error { return m.err }

// Close marks the pane gone, so a still-flying result is discarded.
func (m *Model) Close() { m.closed = true }

// Indices reports the number of listed indices and aliases (tests).
func (m *Model) Indices() int { return len(m.indices) }

// SelectedIndex returns the loaded index's name, "" before the first load.
func (m *Model) SelectedIndex() string {
	if m.sel < 0 || m.sel >= len(m.indices) {
		return ""
	}
	return m.indices[m.sel].Name
}

// CursorIndex returns the index under the sidebar cursor (the one q/s act on
// while the sidebar has the region focus), falling back to the loaded one.
func (m *Model) CursorIndex() string {
	i := m.icur
	if m.region == regionGrid {
		i = m.sel
	}
	if i < 0 || i >= len(m.indices) {
		return ""
	}
	return m.indices[i].Name
}

// PageFrom returns the loaded page's first-hit offset (tests).
func (m *Model) PageFrom() int {
	if m.res == nil {
		return 0
	}
	return m.res.From
}

// PageRows reports the loaded page's hit count (tests).
func (m *Model) PageRows() int {
	if m.res == nil {
		return 0
	}
	return len(m.res.Rows)
}

// GridCursor returns the row cursor's index within the loaded page (tests).
func (m *Model) GridCursor() int { return m.rowCur }

// GridTop returns the grid's first visible row within the loaded page (tests).
func (m *Model) GridTop() int { return m.rowTop }

// InGrid reports whether the grid half holds the region focus (tests).
func (m *Model) InGrid() bool { return m.region == regionGrid }

// Opening reports whether the background connect is still out (tests).
func (m *Model) Opening() bool { return m.opening }

// Loading reports whether a page fetch is in flight (tests).
func (m *Model) Loading() bool { return m.loading }

// Query returns the index's active query body, "" for match-all (tests).
func (m *Model) Query(index string) string { return m.queries[index] }

// SetSize records the pane interior in cells.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h; m.clampScroll() }

// SetFocused records focus for the header and the selection highlight.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the theme palette.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// pendingQuery is one queued RunQuery, held until the index listing exists.
type pendingQuery struct{ index, body string }

// QueueQuery records a query to run as soon as the cluster listing lands —
// the run-from-editor path may have just opened the pane, whose background
// connect is still out.
func (m *Model) QueueQuery(index, body string) {
	m.pending = &pendingQuery{index: index, body: body}
}

// RunQuery installs body as the index's active query and reloads the grid on
// it — the landing point of "run" in a query buffer. The index must be in the
// sidebar; ok reports whether it was found (the caller owns user notices).
func (m *Model) RunQuery(index, body string) (tea.Cmd, bool) {
	at := -1
	for i := range m.indices {
		if m.indices[i].Name == index {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, false
	}
	m.queries[index] = body
	m.sel, m.icur = at, at
	m.region = regionGrid
	m.rowCur, m.rowTop, m.colOff = 0, 0, 0
	m.res, m.pageErr = nil, nil
	return tea.Batch(m.forceFetchCmd(0), m.mappingCmd(index, false)), true
}

// Update handles one message: key presses, focus-filtered by the pane layer,
// and the background results the root model routes here by pane key.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case ResultMsg:
		return m.applyResult(msg)
	}
	return nil
}

// handleKey runs one key. A failed open leaves one live key: r retries the
// connect — endpoints die transiently in a way database files do not.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.err != nil {
		if msg.String() == "r" {
			m.started, m.err = false, nil
			return m.Init()
		}
		return nil
	}
	if m.client == nil {
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
		return m.showMapping()
	case "q":
		return m.openQuery()
	}
	if m.region == regionSidebar {
		return m.sidebarKey(msg)
	}
	return m.gridKey(msg)
}

// sidebarKey navigates the index list; enter/l loads the selection and moves
// to the grid.
func (m *Model) sidebarKey(msg tea.KeyPressMsg) tea.Cmd {
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(msg.String(), &m.icur, len(m.indices), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return nil
	}
	switch msg.String() {
	case "enter", "l", "right":
		var cmd tea.Cmd
		if m.icur != m.sel {
			cmd = m.loadIndex(m.icur)
		}
		m.region = regionGrid
		return cmd
	}
	return nil
}

// gridKey navigates the loaded page: j/k rows (crossing a page edge fetches
// the neighbour page), h/l columns, pgup/pgdown a screenful, n/p (ctrl+f /
// ctrl+b) whole pages, g/G first/last page, enter/v the hit's JSON document,
// a the aggregations, r a re-run of the active query.
func (m *Model) gridKey(msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	switch msg.String() {
	case "j", "down":
		cmd = m.rowStep(1)
	case "k", "up":
		cmd = m.rowStep(-1)
	case "h", "left":
		if m.colOff > 0 {
			m.colOff--
		} else {
			// At the left edge h falls through to the sidebar, the pane's
			// spatial layout.
			m.region = regionSidebar
		}
	case "l", "right":
		if m.res != nil && m.colOff < len(m.res.Columns)-1 {
			m.colOff++
		}
	case "n", "ctrl+f":
		cmd = m.pageStep(1)
	case "p", "ctrl+b":
		cmd = m.pageStep(-1)
	case "pgdown":
		cmd = m.screenStep(1)
	case "pgup":
		cmd = m.screenStep(-1)
	case "g", "home":
		m.rowCur, m.rowTop = 0, 0
		cmd = m.fetchCmd(0)
	case "G", "end":
		cmd = m.lastPage()
	case "enter", "v":
		cmd = m.showHit()
	case "a":
		cmd = m.showAggs()
	case "r":
		cmd = m.fetchCmd(m.pageFromOr0())
	}
	m.clampScroll()
	return cmd
}

// loadIndex selects index i and fetches its first page, plus the mapping in
// the background — it feeds the query buffer's completion and the s view.
func (m *Model) loadIndex(i int) tea.Cmd {
	if i < 0 || i >= len(m.indices) {
		return nil
	}
	m.sel, m.icur = i, i
	m.rowCur, m.rowTop, m.colOff = 0, 0, 0
	m.res, m.pageErr = nil, nil
	return tea.Batch(m.forceFetchCmd(0), m.mappingCmd(m.indices[i].Name, false))
}

// rowStep moves the row cursor, fetching the adjacent page when the cursor
// walks off the loaded one and more hits exist on that side.
func (m *Model) rowStep(d int) tea.Cmd {
	if m.res == nil {
		return nil
	}
	next := m.rowCur + d
	if next >= 0 && next < len(m.res.Rows) {
		m.rowCur = next
		return nil
	}
	if d > 0 && m.hasNextPage() {
		return m.fetchCmd(m.res.From + PageSize)
	}
	if d < 0 && m.res.From > 0 {
		cmd := m.fetchCmd(maxInt(0, m.res.From-PageSize))
		m.pendingBottom = cmd != nil
		return cmd
	}
	return nil
}

// screenStep pages the row cursor by one screenful within the loaded page;
// only a cursor already sitting on the page's edge crosses it.
func (m *Model) screenStep(d int) tea.Cmd {
	if m.res == nil {
		return nil
	}
	h := m.gridHeight()
	if d > 0 {
		if next := m.rowCur + h; next < len(m.res.Rows) {
			m.rowCur = next
			return nil
		}
		if m.rowCur < len(m.res.Rows)-1 {
			m.rowCur = len(m.res.Rows) - 1
			return nil
		}
		if m.hasNextPage() {
			return m.fetchCmd(m.res.From + PageSize)
		}
		return nil
	}
	if next := m.rowCur - h; next >= 0 {
		m.rowCur = next
		return nil
	}
	if m.rowCur > 0 {
		m.rowCur = 0
		return nil
	}
	if m.res.From > 0 {
		cmd := m.fetchCmd(maxInt(0, m.res.From-PageSize))
		m.pendingBottom = cmd != nil
		return cmd
	}
	return nil
}

// pageStep fetches the next or previous page wholesale.
func (m *Model) pageStep(d int) tea.Cmd {
	if m.res == nil {
		return nil
	}
	if d > 0 && m.hasNextPage() {
		return m.fetchCmd(m.res.From + PageSize)
	}
	if d < 0 && m.res.From > 0 {
		return m.fetchCmd(maxInt(0, m.res.From-PageSize))
	}
	return nil
}

// lastPage jumps to the final page. Totals are tracked exactly on every
// search (esq forces track_total_hits), so the jump has a number to aim at;
// it stays inert on the rare lower-bound total.
func (m *Model) lastPage() tea.Cmd {
	if m.res == nil || !m.res.Exact || m.res.Total <= 0 {
		return nil
	}
	last := (maxInt(int(m.res.Total)-1, 0) / PageSize) * PageSize
	return m.fetchCmd(last)
}

// hasNextPage reports whether hits exist past the loaded page.
func (m *Model) hasNextPage() bool {
	if m.res == nil {
		return false
	}
	if m.res.Exact {
		return int64(m.res.From+len(m.res.Rows)) < m.res.Total
	}
	return len(m.res.Rows) == PageSize
}

// pageFromOr0 is the loaded page's offset, 0 before the first result.
func (m *Model) pageFromOr0() int {
	if m.res == nil {
		return 0
	}
	return m.res.From
}

// showMapping shows the target index's mapping read-only: instantly from the
// cache the background fetch filled, else after its own fetch.
func (m *Model) showMapping() tea.Cmd {
	name := m.CursorIndex()
	if name == "" {
		return nil
	}
	if pretty, ok := m.mappings[name]; ok {
		msg := ShowJSONMsg{Endpoint: m.endpoint, Name: name + ".mapping", JSON: pretty}
		return func() tea.Msg { return msg }
	}
	return m.mappingCmd(name, true)
}

// openQuery asks the app to open the target index's query buffer.
func (m *Model) openQuery() tea.Cmd {
	name := m.CursorIndex()
	if name == "" {
		return nil
	}
	msg := OpenQueryMsg{Endpoint: m.endpoint, Index: name}
	return func() tea.Msg { return msg }
}

// showHit shows the cursor row's full document — the fallback for nested
// documents the grid can only render as compact JSON cells.
func (m *Model) showHit() tea.Cmd {
	if m.res == nil || m.rowCur < 0 || m.rowCur >= len(m.res.Hits) {
		return nil
	}
	name := m.SelectedIndex()
	msg := ShowJSONMsg{
		Endpoint: m.endpoint,
		Name:     fmt.Sprintf("%s.hit-%d", name, m.res.From+m.rowCur+1),
		JSON:     esq.PrettyHit(m.res.Hits[m.rowCur]),
	}
	return func() tea.Msg { return msg }
}

// showAggs shows the result's aggregations, when the query asked for any.
func (m *Model) showAggs() tea.Cmd {
	if m.res == nil || m.res.Aggs == "" {
		return nil
	}
	msg := ShowJSONMsg{Endpoint: m.endpoint, Name: m.SelectedIndex() + ".aggs", JSON: m.res.Aggs}
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
	b.WriteString(m.footer(pal))
	return b.String()
}

// errorView is the whole-pane notice for a cluster that would not answer — a
// dead endpoint, refused auth, or an endpoint edited out of the config. The
// state is transient by nature, so the notice names the retry key.
func (m *Model) errorView(pal *theme.Palette) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(pal.Foreground).Render(m.endpoint)
	note := lipgloss.NewStyle().Foreground(pal.Error).Render("cannot reach cluster: " + m.err.Error())
	hint := lipgloss.NewStyle().Faint(true).Render("r retry")
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, title+"\n"+note+"\n"+hint)
}

// headerLine names the endpoint, its index count and the active query.
func (m *Model) headerLine(pal *theme.Palette) string {
	n := len(m.indices)
	counts := fmt.Sprintf("%d ind%s", n, pluralIndex(n))
	if m.opening {
		counts = "connecting…"
	}
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" " + m.endpoint)
	line := title + lipgloss.NewStyle().Faint(true).Render("   "+counts)
	return line + m.queryNote(pal, m.w-lipgloss.Width(line))
}

// queryNote marks a loaded index running under a custom query — the grid is
// not showing "everything", and the header says so like the data viewer's
// filter note.
func (m *Model) queryNote(pal *theme.Palette, room int) string {
	name := m.SelectedIndex()
	if name == "" || strings.TrimSpace(m.queries[name]) == "" || room < 4 {
		return ""
	}
	return lipgloss.NewStyle().Foreground(pal.Warning).Render(gridview.ClipTo("   query: "+name, room))
}

// body joins the sidebar and the grid side by side.
func (m *Model) body(pal *theme.Palette) string {
	h := m.bodyHeight()
	side := m.renderSidebar(pal, h)
	grid := m.renderGrid(pal, h)
	return lipgloss.JoinHorizontal(lipgloss.Top, side, grid) + "\n"
}

// sidebarWidth is the index list's column width.
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

// SidebarWidth is the index list's width in cells (tests).
func (m *Model) SidebarWidth() int { return m.sidebarWidth() }

// renderSidebar draws the index/alias list with doc counts through the shared
// list column (#2468).
func (m *Model) renderSidebar(pal *theme.Palette, height int) string {
	return gridview.Sidebar(m.itop, height, len(m.indices), m.sidebarWidth(),
		func(i, w int) string { return m.sidebarRow(pal, i, w) })
}

// sidebarRow draws one index: marker, name, and right-aligned doc count ("?"
// for an alias, whose count would mean resolving its targets; "a" tags it).
func (m *Model) sidebarRow(pal *theme.Palette, i, w int) string {
	idx := m.indices[i]
	count := "?"
	if idx.Docs >= 0 {
		count = fmt.Sprintf("%d", idx.Docs)
	}
	if idx.Alias {
		count += " a"
	}
	marker := "  "
	if i == m.sel {
		marker = "▸ "
	}
	left := " " + marker + idx.Name
	pad := w - lipgloss.Width(left) - lipgloss.Width(count) - 1
	var line string
	if pad >= 1 {
		line = left + strings.Repeat(" ", pad) + count + " "
	} else {
		line = gridview.ClipTo(left, w)
		if d := w - lipgloss.Width(line); d > 0 {
			line += strings.Repeat(" ", d)
		}
	}
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	if idx.Alias {
		style = style.Foreground(pal.Accent)
	}
	if i == m.icur && m.region == regionSidebar {
		if m.focused {
			style = style.Background(pal.Selection).Bold(true)
		} else {
			style = style.Background(pal.SelectionMuted)
		}
	}
	return style.Render(line)
}

// renderGrid draws the loaded page: header row, then the scrolled hit rows.
func (m *Model) renderGrid(pal *theme.Palette, height int) string {
	w := m.w - m.sidebarWidth() - 1
	if w < 4 {
		return ""
	}
	sep := lipgloss.NewStyle().Faint(true).Render("│")
	var b strings.Builder
	empty := m.res == nil || len(m.res.Columns) == 0
	switch {
	case m.pageErr != nil:
		b.WriteString(sep + lipgloss.NewStyle().Foreground(pal.Error).Render(gridview.ClipTo(" "+m.pageErr.Error(), w)))
	case m.opening:
		b.WriteString(sep + lipgloss.NewStyle().Faint(true).Render(" connecting to "+m.endpoint+"…"))
	case m.loading && empty:
		b.WriteString(sep + lipgloss.NewStyle().Faint(true).Render(" searching…"))
	case m.sel < 0:
		b.WriteString(sep + lipgloss.NewStyle().Faint(true).Render(" no index selected"))
	case empty:
		b.WriteString(sep + lipgloss.NewStyle().Faint(true).Render(" no hits"))
	}
	if m.pageErr != nil || m.opening || m.sel < 0 || empty {
		for k := 1; k < height; k++ {
			b.WriteString("\n" + sep)
		}
		return b.String()
	}
	widths := m.colWidths()
	b.WriteString(sep + gridview.HeaderRow(pal, m.res.Columns, widths, m.colOff, w))
	rows := height - 1
	for k := 0; k < rows; k++ {
		b.WriteString("\n" + sep)
		if i := m.rowTop + k; i < len(m.res.Rows) {
			b.WriteString(m.dataRow(pal, i, widths, w))
		}
	}
	return b.String()
}

// colWidths sizes the visible columns from the header and the loaded page,
// clamped to maxColWidth.
func (m *Model) colWidths() []int {
	widths := make([]int, len(m.res.Columns))
	for i, c := range m.res.Columns {
		widths[i] = lipgloss.Width(c)
	}
	for _, row := range m.res.Rows {
		for i, cell := range row {
			t := cell.Text
			if cell.Null {
				t = gridview.NullCell
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

// dataRow draws one hit row through the shared grid renderer (#2468); the
// row cursor highlights only while the grid region has the focus.
func (m *Model) dataRow(pal *theme.Palette, i int, widths []int, w int) string {
	selected := i == m.rowCur && m.region == regionGrid
	return gridview.DataRow(pal, cells(m.res.Rows[i]), widths, m.colOff, w, selected, m.focused)
}

// cells converts one hit row to the renderer's cell type, so internal/esq
// stays free of a UI dependency.
func cells(row []esq.Cell) []gridview.Cell {
	out := make([]gridview.Cell, len(row))
	for i, c := range row {
		out[i] = gridview.Cell{Text: c.Text, Null: c.Null}
	}
	return out
}

// footer is the status line: hit window and total, the fetch in flight, an
// aggregations marker, and the key hints that fit.
func (m *Model) footer(pal *theme.Palette) string {
	status := ""
	switch {
	case m.opening:
		status = "connecting…"
	case m.loading:
		status = "searching…"
	case m.res != nil && m.pageErr == nil:
		n := len(m.res.Rows)
		if n == 0 {
			status = "0 hits"
		} else {
			first := m.res.From + 1
			last := m.res.From + n
			total := fmt.Sprintf("%d", m.res.Total)
			if !m.res.Exact {
				// A lower bound must never read as a count.
				total = fmt.Sprintf("≥%d", m.res.Total)
			}
			status = fmt.Sprintf("hits %d–%d of %s", first, last, total)
		}
		if m.res.Aggs != "" {
			status += " · aggs: a"
		}
	}
	hints := "tab switch · enter open · j/k row · h/l column · n/p page · q query · s mapping · v doc"
	line := " " + status
	if status != "" {
		line += " · "
	}
	line += hints
	return lipgloss.NewStyle().Faint(true).Render(gridview.ClipTo(line, m.w))
}

// bodyHeight is the room between the header and footer lines.
func (m *Model) bodyHeight() int {
	if h := m.h - 2; h >= 1 {
		return h
	}
	return 1
}

// gridHeight is how many hit rows the grid shows: the body minus the line it
// spends on the column headers.
func (m *Model) gridHeight() int {
	if h := m.bodyHeight() - 1; h >= 1 {
		return h
	}
	return 1
}

// clampScroll keeps both cursors valid and inside their visible windows
// through the shared list window (#2462); the column offset is the one clamp
// of its own, since it scrolls columns rather than a selection.
func (m *Model) clampScroll() {
	rows := 0
	cols := 0
	if m.res != nil {
		rows, cols = len(m.res.Rows), len(m.res.Columns)
	}
	ui.ClampWindow(&m.icur, &m.itop, len(m.indices), m.bodyHeight())
	ui.ClampWindow(&m.rowCur, &m.rowTop, rows, m.gridHeight())
	if m.colOff < 0 {
		m.colOff = 0
	}
	if cols > 0 && m.colOff >= cols {
		m.colOff = cols - 1
	}
}

// pluralIndex is the index-count suffix ("1 index", "2 indices").
func pluralIndex(n int) string {
	if n == 1 {
		return "ex"
	}
	return "ices"
}

// maxInt is the int max.
func maxInt(a, b int) int {
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

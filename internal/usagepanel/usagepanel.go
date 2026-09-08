// Package usagepanel is the Usage tool window (#2552): a singleton
// bottom-split pane that answers "what did I do" from the local usage log
// alone. Four tabs pick the question — Commands (top commands split by
// dispatch source), Keys (chords that found no binding, per focus context),
// Palette (dismissal rates per palette mode, with the query/results split)
// and Ops (long-running operations and slow dispatches) — and a Today / Week
// / Month period selector picks the range, exactly like the Time window.
//
// The pane is a pure consumer: the root model reads and aggregates the log in
// a background command (the same telemetry.Reader the Time window uses) and
// hands the finished telemetry.Report over via Set. Nothing here touches the
// filesystem, and nothing anywhere uploads — the report is read-only and
// local (#2235). 'e' hands the current tab's CSV to the root model, which
// writes it to a scratch file.
package usagepanel

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/filterbar"
	"ike/internal/filterexpr"
	"ike/internal/telemetry"
	"ike/internal/theme"
	"ike/internal/ui"
)

// Tab is one of the pane's questions.
type Tab int

const (
	// TabCommands lists the top commands by dispatch source.
	TabCommands Tab = iota
	// TabKeys lists the unbound chords per focus context.
	TabKeys
	// TabPalette lists the palette dismissal rates per mode.
	TabPalette
	// TabOps lists the long-running operations and slow dispatches.
	TabOps
)

// tabNames labels the tabs in display order.
var tabNames = []string{"Commands", "Keys", "Palette", "Ops"}

// Period is one of the pane's date ranges.
type Period int

const (
	// PeriodToday is the current local day.
	PeriodToday Period = iota
	// PeriodWeek is the last seven days, today included.
	PeriodWeek
	// PeriodMonth is the last thirty days, today included.
	PeriodMonth
)

// periodNames labels the periods in display order.
var periodNames = []string{"Today", "Week", "Month"}

// periodDays is how many days back each period reaches, today included.
var periodDays = []int{1, 7, 30}

// RefreshMsg asks the root model to re-read the log ('r').
type RefreshMsg struct{}

// ExportMsg asks the root model to write the current tab to a CSV scratch
// file ('e'). CSV is built here because the pane owns what "the current
// view" means; the root model owns where a scratch file goes.
type ExportMsg struct {
	// Label names the exported tab and period for the notification
	// ("Commands · Week").
	Label string
	// CSV is the complete file content, header row included.
	CSV string
}

// Schema is the pane's filter language: free match text over the row's
// name column (command id, chord, mode, op id).
var Schema = filterexpr.Schema{}

// Row is one rendered line of the current tab, kept alongside the cells so
// the filter, the CSV export and the view all agree on what a row is.
type Row struct {
	// Name is the row's identity: command id, "context chord", palette
	// mode or op id. The filter matches against it.
	Name string
	// Cells are the CSV columns, Name first.
	Cells []string
	// Faint marks a secondary row (a removed-by-config chord, a slow
	// dispatch under the ops).
	Faint bool
}

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the other tool windows.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	report  *telemetry.Report
	loading bool
	tab     Tab
	period  Period

	filter filterbar.Model
	rows   []Row
	cursor int
	top    int

	clicks ui.ClickTracker
	now    func() time.Time
}

// New returns an empty panel; the report arrives via Set.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, now: time.Now, filter: filterbar.New(Schema)}
}

// SetNow injects the clock (tests).
func (m *Model) SetNow(f func() time.Time) {
	if f != nil {
		m.now = f
		m.Refresh()
	}
}

// Set replaces the aggregated report and rebuilds the rows.
func (m *Model) Set(r *telemetry.Report) {
	m.report = r
	m.loading = false
	m.Refresh()
}

// SetLoading flags an in-flight background read for the header.
func (m *Model) SetLoading(b bool) { m.loading = b }

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the panel focused (header + selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// Tab reports the selected question.
func (m *Model) Tab() Tab { return m.tab }

// SetTab selects a question and rebuilds the rows.
func (m *Model) SetTab(t Tab) {
	if t < TabCommands || t > TabOps {
		return
	}
	m.tab = t
	m.Refresh()
}

// Period reports the selected range.
func (m *Model) Period() Period { return m.period }

// SetPeriod selects a range and rebuilds the rows.
func (m *Model) SetPeriod(p Period) {
	if p < PeriodToday || p > PeriodMonth {
		return
	}
	m.period = p
	m.Refresh()
}

// Rows exposes the rendered rows (tests).
func (m *Model) Rows() []Row { return m.rows }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// Range is the inclusive day range of the selected period, ending today.
func (m *Model) Range() (time.Time, time.Time) {
	now := m.clock()
	back := periodDays[m.period] - 1
	return now.AddDate(0, 0, -back), now
}

// Refresh re-derives the rows from the report for the selected tab and
// period, keeping the cursor on the same row where possible.
func (m *Model) Refresh() {
	keep := ""
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		keep = m.rows[m.cursor].Name
	}
	from, to := m.Range()
	sum := m.report.UsageRange(from, to)
	all := tabRows(m.tab, sum)
	m.rows = m.rows[:0]
	for _, r := range all {
		if m.matches(r) {
			m.rows = append(m.rows, r)
		}
	}
	m.cursor = 0
	for i, r := range m.rows {
		if r.Name == keep {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

// header lists the CSV / column header of one tab.
func header(t Tab) []string {
	switch t {
	case TabCommands:
		return []string{"command", "total", "keybind", "palette", "menu", "mouse"}
	case TabKeys:
		return []string{"context", "chord", "misses", "removed_default"}
	case TabPalette:
		return []string{"mode", "opens", "picks", "dismissed", "rate", "with_query", "no_query", "no_results", "avg_open"}
	}
	return []string{"kind", "id", "started", "ok", "error", "canceled", "avg", "max"}
}

// tabRows flattens the summary slice a tab shows into rows.
func tabRows(t Tab, s telemetry.UsageSummary) []Row {
	var out []Row
	switch t {
	case TabCommands:
		for _, c := range s.Commands {
			out = append(out, Row{Name: c.ID, Cells: []string{c.ID, itoa(c.N), itoa(c.Keybind), itoa(c.Palette), itoa(c.Menu), itoa(c.Mouse)}})
		}
	case TabKeys:
		for _, u := range s.Unbound {
			out = append(out, Row{Name: u.Context + " " + u.Chord, Faint: u.Removed != "",
				Cells: []string{u.Context, u.Chord, itoa(u.N), u.Removed}})
		}
	case TabPalette:
		for _, p := range s.Palette {
			out = append(out, Row{Name: p.Mode, Cells: []string{p.Mode, itoa(p.Opens), itoa(p.Picks), itoa(p.Dismissed),
				percent(p.Rate), itoa(p.WithQuery), itoa(p.NoQuery), itoa(p.NoResults), telemetry.FormatMs(p.AvgOpen)}})
		}
	case TabOps:
		for _, o := range s.Ops {
			out = append(out, Row{Name: o.ID, Cells: []string{"op", o.ID, itoa(o.Started), itoa(o.OK), itoa(o.Errors), itoa(o.Canceled),
				telemetry.FormatMs(o.Avg), telemetry.FormatMs(o.Max)}})
		}
		for _, c := range s.Slow {
			out = append(out, Row{Name: c.ID, Faint: true, Cells: []string{"dispatch", c.ID, itoa(c.N), itoa(c.N - c.Failed), itoa(c.Failed), "0",
				telemetry.FormatMs(c.Avg), telemetry.FormatMs(c.Max)}})
		}
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }

// percent renders a 0..1 rate as a whole percentage.
func percent(r float64) string {
	return strconv.Itoa(int(r*100+0.5)) + "%"
}

// matches gates one row through the filter's free text.
func (m *Model) matches(r Row) bool {
	q := m.filter.Query()
	if q.Empty() {
		return true
	}
	_, ok := filterexpr.MatchText(q.Match, r.Name)
	return ok
}

// Update handles one message while the panel exists; only key presses reach
// it, focus-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return m.handleKey(k)
	}
	return nil
}

// Filtering reports whether the filter row holds the keyboard (#2409).
func (m *Model) Filtering() bool { return m.filter.Active() }

// OpenSearch implements the pane's Searchable capability (#2409): the shared
// find chord focuses the filter row, exactly as "/" does.
func (m *Model) OpenSearch() bool {
	m.filter.Focus()
	return true
}

// NextMatch implements the pane's match-step capability (#2410).
func (m *Model) NextMatch() ui.MatchStep { return m.stepFiltered(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.stepFiltered(-1) }

func (m *Model) stepFiltered(delta int) ui.MatchStep {
	if !m.filter.Active() {
		return ui.NoStep
	}
	next, st := ui.StepOver(m.cursor, len(m.rows), delta, func(int) bool { return true })
	m.cursor = next
	m.clampScroll()
	return m.filter.ShowStep(st)
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.filter.Active() {
		handled, changed := m.filter.Key(msg)
		if changed {
			m.Refresh()
		}
		if handled {
			return nil
		}
	}
	if ui.FindChord(msg.String()) {
		m.OpenSearch()
		return nil
	}
	if ui.ListNav(msg.String(), &m.cursor, len(m.rows), m.bodyHeight(), ui.NavFull) {
		m.clampScroll()
		return nil
	}
	switch msg.String() {
	case "/":
		m.filter.Focus()
	case "tab", "l", "right":
		m.SetTab(Tab((int(m.tab) + 1) % len(tabNames)))
	case "shift+tab", "h", "left":
		m.SetTab(Tab((int(m.tab) + len(tabNames) - 1) % len(tabNames)))
	case "p":
		m.SetPeriod(Period((int(m.period) + 1) % len(periodNames)))
	case "P":
		m.SetPeriod(Period((int(m.period) + len(periodNames) - 1) % len(periodNames)))
	case "e":
		return m.export()
	case "r":
		return func() tea.Msg { return RefreshMsg{} }
	}
	m.clampScroll()
	return nil
}

// export builds the current tab's CSV and hands it to the root model.
func (m *Model) export() tea.Cmd {
	csv := m.CSV()
	label := tabNames[m.tab] + " · " + periodNames[m.period]
	return func() tea.Msg { return ExportMsg{Label: label, CSV: csv} }
}

// CSV renders the current tab as comma-separated values: one row per visible
// line, the period repeated on every row so a concatenated export stays
// self-describing.
func (m *Model) CSV() string {
	from, to := m.Range()
	var b strings.Builder
	writeCSVRow(&b, append([]string{"tab", "period", "from", "to"}, header(m.tab)...))
	for _, r := range m.rows {
		writeCSVRow(&b, append([]string{tabNames[m.tab], periodNames[m.period],
			from.Format(telemetry.DayFormat), to.Format(telemetry.DayFormat)}, r.Cells...))
	}
	return b.String()
}

// writeCSVRow appends one quoted row.
func writeCSVRow(b *strings.Builder, fields []string) {
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(csvField(f))
	}
	b.WriteByte('\n')
}

// csvField quotes a value that would otherwise break the row.
func csvField(s string) string {
	if !strings.ContainsAny(s, ",\"\n") {
		return s
	}
	return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
}

// View renders the header with its tab bar and period selector, the filter
// row, the column header, the scrolled rows and the key hints.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	b.WriteString(m.filter.View(m.width, pal))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(m.clip(m.columnLine(header(m.tab)))))
	b.WriteString("\n")
	b.WriteString(m.renderRows(pal, m.bodyHeight()))
	b.WriteString(m.footer())
	return b.String()
}

// headerLine names the pane, draws the tab bar and the period selector.
func (m *Model) headerLine(pal *theme.Palette) string {
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" Usage")
	sel := func(names []string, on int) string {
		var b strings.Builder
		for i, name := range names {
			if i > 0 {
				b.WriteString(" ")
			}
			// Reverse video, not an underline: lipgloss renders an
			// underlined run one escape sequence per cell, which would break
			// every "does the header say Keys" assertion for no visual gain.
			st := lipgloss.NewStyle().Faint(true)
			if i == on {
				st = lipgloss.NewStyle().Foreground(pal.Background).Background(pal.Accent).Bold(true)
			}
			b.WriteString(st.Render(name))
		}
		return b.String()
	}
	count := strconv.Itoa(len(m.rows)) + " rows"
	if m.loading {
		count = "reading log… · " + count
	}
	return title + "   " + sel(tabNames, int(m.tab)) + "   " + sel(periodNames, int(m.period)) +
		lipgloss.NewStyle().Faint(true).Render("   "+count)
}

// widths lays the columns out: the name columns wide, the numbers narrow.
func widths(t Tab) []int {
	switch t {
	case TabCommands:
		return []int{34, 6, 8, 8, 6, 6}
	case TabKeys:
		return []int{18, 22, 7, 24}
	case TabPalette:
		return []int{5, 6, 6, 10, 5, 11, 9, 11, 9}
	}
	return []int{9, 24, 8, 5, 6, 9, 8, 8}
}

// columnLine joins cells into one padded line: the first column (and the
// second on the Keys / Ops tabs, which have two name columns) left-aligned,
// the rest right-aligned.
func (m *Model) columnLine(cells []string) string {
	ws := widths(m.tab)
	nameCols := 1
	if m.tab == TabKeys || m.tab == TabOps {
		nameCols = 2
	}
	var b strings.Builder
	b.WriteString(" ")
	for i, c := range cells {
		if i >= len(ws) {
			break
		}
		if i > 0 {
			b.WriteString("  ")
		}
		if i < nameCols {
			b.WriteString(pad(c, ws[i]))
		} else {
			b.WriteString(padLeft(c, ws[i]))
		}
	}
	return b.String()
}

// renderRows draws the list scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	m.clampScroll()
	empty := lipgloss.NewStyle().Faint(true).Render(" " + m.emptyText())
	return ui.RenderWindow(m.top, height, len(m.rows), empty, func(i int) string { return m.renderRow(pal, i) })
}

// emptyText explains an empty list per load state and filter.
func (m *Model) emptyText() string {
	if m.loading {
		return "(reading the usage log…)"
	}
	if !m.filter.Empty() {
		return "(no row matches the filter)"
	}
	if m.report == nil || m.report.Files == 0 {
		return "(no usage log yet — telemetry.enabled records one)"
	}
	switch m.tab {
	case TabKeys:
		return "(no unbound chords in this period)"
	case TabPalette:
		return "(no palette opens in this period)"
	case TabOps:
		return "(no operations or slow dispatches in this period)"
	}
	return "(no commands in this period)"
}

// renderRow draws one line.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	r := m.rows[i]
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	if r.Faint {
		style = style.Faint(true)
	}
	if i == m.cursor {
		if m.focused {
			style = style.Background(pal.Selection).Bold(true)
		} else {
			style = style.Background(pal.SelectionMuted)
		}
	}
	return style.Render(m.clip(m.columnLine(r.Cells)))
}

// footer shows the key hints.
func (m *Model) footer() string {
	return lipgloss.NewStyle().Faint(true).Render(
		m.clip(" tab view · p period · e export CSV · r reload · / filter · j/k move"))
}

// chromeRows counts the header, filter, column-header and footer lines.
const chromeRows = 4

// bodyHeight is the room the list gets between the chrome lines.
func (m *Model) bodyHeight() int {
	h := m.height - chromeRows
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll keeps the cursor valid and inside the visible window.
func (m *Model) clampScroll() {
	ui.ClampWindow(&m.cursor, &m.top, len(m.rows), m.bodyHeight())
}

// clip bounds one rendered line to the panel width.
func (m *Model) clip(s string) string {
	if m.width > 0 && len([]rune(s)) > m.width {
		return string([]rune(s)[:m.width-1]) + "…"
	}
	return s
}

// pad right-pads a cell to width, truncating what does not fit.
func pad(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		return string(r[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-len(r))
}

// padLeft right-aligns a cell in width.
func padLeft(s string, w int) string {
	r := []rune(s)
	if len(r) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(r)) + s
}

// clock resolves the injected clock with the real one.
func (m *Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// Mouse control, mirroring the Time pane: y 0 is the header, y 1 the filter
// row, y 2 the column header, list rows start at y 3.
const headerRows = 3

// Wheel scrolls the list by delta rows (positive = down).
func (m *Model) Wheel(delta int) {
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows), m.bodyHeight())
}

// Click selects a row; a click on the header's tab bar switches the view, a
// click on the period selector the range.
func (m *Model) Click(x, y int) tea.Cmd {
	if y == 0 {
		m.clickHeader(x)
		return nil
	}
	i, ok := ui.RowAt(y, m.top, headerRows, m.bodyHeight(), len(m.rows))
	if !ok {
		m.clicks.Reset()
		return nil
	}
	m.clicks.Double(i, m.now())
	m.cursor = i
	return nil
}

// clickHeader maps a header-row x to a tab or a period. The tab bar starts
// after the " Usage" title and its three-space gap, the period selector
// three spaces after the last tab (headerLine).
func (m *Model) clickHeader(x int) {
	start := len(" Usage") + 3
	for i, name := range tabNames {
		end := start + len(name)
		if x >= start && x < end {
			m.SetTab(Tab(i))
			return
		}
		start = end + 1
	}
	start += 2
	for i, name := range periodNames {
		end := start + len(name)
		if x >= start && x < end {
			m.SetPeriod(Period(i))
			return
		}
		start = end + 1
	}
}

// PasteText inserts a pasted block into the open filter row at its cursor
// (#2460), re-deriving the rows exactly like typing there does. A closed
// filter row lets the paste fall through.
func (m *Model) PasteText(text string) bool {
	if !m.filter.Active() {
		return false
	}
	if !m.filter.Paste(text) {
		return false
	}
	m.Refresh()
	return true
}

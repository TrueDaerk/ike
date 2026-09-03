// Package timepanel is the Time tool window (#2426): a singleton
// bottom-split pane that answers "how long did I work on which project" from
// the local usage log alone. Rows are projects, columns are active time,
// session count and the five commands dispatched most often there; the Today
// / Week / Month tabs pick the date range, and a per-day ASCII bar breaks the
// selected project's range down.
//
// The pane is a pure consumer, like the Dependencies window: the root model
// reads and aggregates the log in a background command and hands the finished
// telemetry.Report over via Set. Nothing here touches the filesystem, and
// nothing anywhere uploads — the report is read-only and local (#2235).
package timepanel

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

// Tab is one of the pane's date ranges.
type Tab int

const (
	// TabToday is the current local day.
	TabToday Tab = iota
	// TabWeek is the last seven days, today included.
	TabWeek
	// TabMonth is the last thirty days, today included.
	TabMonth
)

// tabNames labels the tabs in display order.
var tabNames = []string{"Today", "Week", "Month"}

// tabDays is how many days back each tab reaches, today included.
var tabDays = []int{1, 7, 30}

// TopCommands is how many command ids a row lists — the five that dominate a
// project's usage say what the work *was*; a longer list only crowds the row.
const TopCommands = 5

// RefreshMsg asks the root model to re-read the log ('r').
type RefreshMsg struct{}

// ExportMsg asks the root model to write the current view to a CSV scratch
// file ('e'). CSV is built here because the pane owns what "the current view"
// means; the root model owns where a scratch file goes.
type ExportMsg struct {
	// Label names the exported range for the notification ("Today").
	Label string
	// CSV is the complete file content, header row included.
	CSV string
}

// Schema is the pane's filter language: a project gate plus free match text
// over the project name.
var Schema = filterexpr.Schema{Fields: []filterexpr.Field{
	{Name: "project", ValueDoc: "a project name or substring", Doc: "project"},
}}

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

	filter filterbar.Model
	rows   []telemetry.Summary
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

// Tab reports the selected range (tests).
func (m *Model) Tab() Tab { return m.tab }

// SetTab selects a range and rebuilds the rows.
func (m *Model) SetTab(t Tab) {
	if t < TabToday || t > TabMonth {
		return
	}
	m.tab = t
	m.Refresh()
}

// Rows reports the visible project count (tests).
func (m *Model) Rows() int { return len(m.rows) }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// Summaries exposes the rendered rows (tests).
func (m *Model) Summaries() []telemetry.Summary { return m.rows }

// Range is the inclusive day range of the selected tab, ending today.
func (m *Model) Range() (time.Time, time.Time) {
	now := m.clock()
	back := tabDays[m.tab] - 1
	return now.AddDate(0, 0, -back), now
}

// Refresh re-derives the rows from the report for the selected range,
// keeping the cursor on the same project where possible.
func (m *Model) Refresh() {
	keep := ""
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		keep = m.rows[m.cursor].Name
	}
	from, to := m.Range()
	all := m.report.Range(from, to)
	m.rows = m.rows[:0]
	for _, s := range all {
		if !m.matches(s) {
			continue
		}
		if len(s.Commands) > TopCommands {
			s.Commands = s.Commands[:TopCommands]
		}
		m.rows = append(m.rows, s)
	}
	m.cursor = 0
	for i, s := range m.rows {
		if s.Name == keep {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

// matches gates one project row through the filter.
func (m *Model) matches(s telemetry.Summary) bool {
	q := m.filter.Query()
	if q.Empty() {
		return true
	}
	if names := q.Values("project"); len(names) > 0 && !anyMatch(names, s.Name) {
		return false
	}
	if _, ok := filterexpr.MatchText(q.Match, s.Name); !ok {
		return false
	}
	return true
}

// anyMatch ORs the project: values against the rendered name.
func anyMatch(pats []string, name string) bool {
	for _, p := range pats {
		if strings.Contains(strings.ToLower(name), strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// Total is the selected range's active time across the visible rows.
func (m *Model) Total() time.Duration {
	var d time.Duration
	for _, s := range m.rows {
		d += s.Active
	}
	return d
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
	case "e":
		return m.export()
	case "r":
		return func() tea.Msg { return RefreshMsg{} }
	}
	m.clampScroll()
	return nil
}

// export builds the current view's CSV and hands it to the root model.
func (m *Model) export() tea.Cmd {
	csv := m.CSV()
	label := tabNames[m.tab]
	return func() tea.Msg { return ExportMsg{Label: label, CSV: csv} }
}

// CSV renders the current view as comma-separated values: one row per visible
// project, the range repeated on every row so a concatenated export stays
// self-describing.
func (m *Model) CSV() string {
	from, to := m.Range()
	var b strings.Builder
	b.WriteString("range,from,to,project,token,active,active_seconds,sessions,top_commands\n")
	for _, s := range m.rows {
		var cmds []string
		for _, c := range s.Commands {
			cmds = append(cmds, c.ID+"="+strconv.Itoa(c.N))
		}
		fields := []string{
			tabNames[m.tab],
			from.Format(telemetry.DayFormat),
			to.Format(telemetry.DayFormat),
			s.Name,
			s.Token,
			telemetry.FormatDuration(s.Active),
			strconv.FormatInt(int64(s.Active/time.Second), 10),
			strconv.Itoa(s.Sessions),
			strings.Join(cmds, " "),
		}
		for i, f := range fields {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(csvField(f))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// csvField quotes a value that would otherwise break the row.
func csvField(s string) string {
	if !strings.ContainsAny(s, ",\"\n") {
		return s
	}
	return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
}

// View renders the header with its tab bar, the filter row, the scrolled
// project rows, the selected project's per-day bar chart and the key hints.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	b.WriteString(m.filter.View(m.width, pal))
	b.WriteString("\n")
	b.WriteString(m.renderRows(pal, m.bodyHeight()))
	b.WriteString(m.renderChart(pal))
	b.WriteString(m.footer(pal))
	return b.String()
}

// headerLine names the pane, draws the tab bar and totals the range.
func (m *Model) headerLine(pal *theme.Palette) string {
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" Time")
	var tabs strings.Builder
	for i, name := range tabNames {
		if i > 0 {
			tabs.WriteString(" ")
		}
		// Reverse video, not an underline: lipgloss renders an underlined
		// run one escape sequence per cell, which would break every "does
		// the header say Week" assertion for no visual gain.
		st := lipgloss.NewStyle().Faint(true)
		if Tab(i) == m.tab {
			st = lipgloss.NewStyle().Foreground(pal.Background).Background(pal.Accent).Bold(true)
		}
		tabs.WriteString(st.Render(name))
	}
	sum := telemetry.FormatDuration(m.Total()) + " active"
	if m.loading {
		sum = "reading log… · " + sum
	}
	return title + "   " + tabs.String() +
		lipgloss.NewStyle().Faint(true).Render("   "+sum)
}

// renderRows draws the project list scrolled around the cursor.
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
		return "(no project matches the filter)"
	}
	if m.report == nil || m.report.Files == 0 {
		return "(no usage log yet — telemetry.enabled records one)"
	}
	return "(no recorded time in this range)"
}

// renderRow draws one project line: name, active time, sessions and the top
// commands.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	s := m.rows[i]
	line := " " + pad(s.Name, 24) + "  " + padLeft(telemetry.FormatDuration(s.Active), 8) +
		"  " + padLeft(strconv.Itoa(s.Sessions), 3) + " sess"
	if top := topCommandText(s.Commands); top != "" {
		line += "  " + top
	}
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	if s.Name == telemetry.UnknownName {
		style = style.Faint(true)
	}
	if i == m.cursor {
		if m.focused {
			style = style.Background(pal.Selection).Bold(true)
		} else {
			style = style.Background(pal.SelectionMuted)
		}
	}
	return style.Render(m.clip(line))
}

// topCommandText renders the command column as "id×n, id×n".
func topCommandText(cs []telemetry.CommandCount) string {
	var parts []string
	for _, c := range cs {
		parts = append(parts, c.ID+"×"+strconv.Itoa(c.N))
	}
	return strings.Join(parts, ", ")
}

// chartRows is how many day bars the chart shows at most; a Month tab has
// thirty days and the pane is a bottom split, so the chart lists the most
// recent days and says how many it left out.
const chartRows = 7

// renderChart draws the selected project's per-day bars, most recent last.
// It is dropped entirely on a short pane — the list is the primary content.
func (m *Model) renderChart(pal *theme.Palette) string {
	h := m.chartHeight()
	if h == 0 {
		return ""
	}
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return strings.Repeat("\n", h)
	}
	s := m.rows[m.cursor]
	days := s.Days
	hidden := 0
	if len(days) > h-1 {
		hidden = len(days) - (h - 1)
		days = days[hidden:]
	}
	var max time.Duration
	for _, d := range days {
		if d.Active > max {
			max = d.Active
		}
	}
	head := " " + s.Name + " — per day"
	if hidden > 0 {
		head += " (last " + strconv.Itoa(len(days)) + " of " + strconv.Itoa(len(days)+hidden) + ")"
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Accent).Render(m.clip(head)) + "\n")
	for _, d := range days {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Info).Render(
			m.clip(" "+d.Day+" "+bar(d.Active, max, m.barWidth())+" "+telemetry.FormatDuration(d.Active))) + "\n")
	}
	return b.String()
}

// barWidth is the room a day bar gets: the pane width minus the date and the
// duration label, clamped to something drawable.
func (m *Model) barWidth() int {
	w := m.width - 12 - 10
	if w < 4 {
		w = 4
	}
	if w > 40 {
		w = 40
	}
	return w
}

// bar renders one ASCII bar scaled against the range's busiest day. A
// non-zero day always draws at least one block, so "a little" never reads as
// "nothing".
func bar(d, max time.Duration, width int) string {
	if max <= 0 || d <= 0 {
		return strings.Repeat(" ", width)
	}
	n := int(int64(width) * int64(d) / int64(max))
	if n < 1 {
		n = 1
	}
	if n > width {
		n = width
	}
	return strings.Repeat("█", n) + strings.Repeat(" ", width-n)
}

// footer shows the key hints.
func (m *Model) footer(pal *theme.Palette) string {
	return lipgloss.NewStyle().Faint(true).Render(
		m.clip(" tab range · e export CSV · r reload · / filter · j/k move"))
}

// chartHeight is the room the bar chart gets: nothing on a short pane, the
// header line plus up to chartRows day bars otherwise.
func (m *Model) chartHeight() int {
	// 3 chrome rows (header, filter, footer) plus at least 3 list rows have
	// to survive before the chart is worth drawing.
	if m.height < 3+3+2 {
		return 0
	}
	h := chartRows + 1
	if room := m.height - 3 - 3; h > room {
		h = room
	}
	return h
}

// bodyHeight is the room the list gets between the chrome and the chart.
func (m *Model) bodyHeight() int {
	h := m.height - 3 - m.chartHeight()
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

// Mouse control, mirroring the Dependencies pane: y 0 is the header, y 1 the
// filter row, list rows start at y 2.
const headerRows = 2

// Wheel scrolls the list by delta rows (positive = down).
func (m *Model) Wheel(delta int) {
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows), m.bodyHeight())
}

// Click selects a row; a click on the header's tab bar switches the range.
func (m *Model) Click(x, y int) tea.Cmd {
	if y == 0 {
		m.clickTab(x)
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

// clickTab maps a header-row x to a tab. The bar starts after the " Time"
// title and its three-space gap (headerLine).
func (m *Model) clickTab(x int) {
	start := len(" Time") + 3
	for i, name := range tabNames {
		end := start + len(name)
		if x >= start && x < end {
			m.SetTab(Tab(i))
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

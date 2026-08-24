// Package testresults is the Test Results tool window (#1911): a singleton
// bottom-split pane showing a structured test run — the JetBrains test runner
// scaled to the terminal. The left column is the result tree (group → test →
// subtest, pass/fail/skip glyphs, durations, a summary header), the right
// column the selected test's buffered output ('o' toggles the whole run's raw
// output instead). It is a pure consumer: the app captures a test run's
// output, parses it through the language's TestSpec.ParseOutput seam
// (internal/lang), and hands the results in; re-run actions go back to the
// app as messages, which relaunches through the ordinary run pipeline.
package testresults

import (
	"image/color"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/lang"
	"ike/internal/theme"
	"ike/internal/ui"
)

// OpenLocationMsg asks the root model to open Path (absolute) and place the
// cursor at the 0-based Line — the same navigation seam the Problems pane
// uses.
type OpenLocationMsg struct {
	Path string
	Line int
}

// LocateTestMsg asks the root model to jump to the source of the test named
// by RerunID — a passed test has no failure location, so the app scans the
// run's test file for the declaration instead.
type LocateTestMsg struct {
	RerunID string
}

// RerunMsg asks the root model to re-run tests of the last captured run:
// every test (zero value), only the previously failed ones (Failed), or a
// single test by its RerunID (ID non-empty).
type RerunMsg struct {
	Failed bool
	ID     string
}

// CopyMsg asks the root model to put Text on the system clipboard; What names
// the payload for the confirmation toast ("test result"). The panel emits it
// instead of writing itself, the seam every pane copy action uses (#2071).
type CopyMsg struct {
	Text string
	What string
}

// node is one result-tree node; res is nil for synthesized parents (groups,
// a subtest's container that never reported itself).
type node struct {
	label    string
	depth    int
	res      *lang.TestResult
	children []*node
	up       *node
}

// row is one rendered tree line.
type row struct {
	n *node
}

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the Problems panel.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	// Last captured run (#1911): the configuration's display name, the run's
	// working directory (failure paths resolve against it) and its parsed
	// results plus the raw combined output.
	configName string
	dir        string
	running    bool
	results    []lang.TestResult
	raw        string

	rows   []row
	cursor int
	top    int

	// pass/fail/skip/elapsed memoize the summary line, maintained on rebuild.
	passed, failed, skipped int
	elapsed                 float64
	// coverage is the run-wide line-coverage percentage of a coverage run
	// (#2081); hasCoverage gates it — a plain run shows none.
	coverage    float64
	hasCoverage bool

	// rawMode shows the whole run's raw output in the detail column instead
	// of the selected test's buffered output ('o' toggles).
	rawMode bool
	// detailFocus routes j/k to the detail column (tab/h/l switch).
	detailFocus bool
	detailTop   int

	// Double-click detection mirrors the Problems panel; now is injectable so
	// tests control the clock.
	lastClickRow int
	lastClickAt  time.Time
	now          func() time.Time
}

// New returns an empty panel; results arrive via StartRun/FinishRun.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, lastClickRow: -1, now: time.Now}
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the panel focused (header + selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// Rows reports the flattened row count (tests).
func (m *Model) Rows() int { return len(m.rows) }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// Running reports whether a captured run is in flight (tests, chrome).
func (m *Model) Running() bool { return m.running }

// ConfigName reports the last run's configuration name (tests, chrome).
func (m *Model) ConfigName() string { return m.configName }

// StartRun marks a captured run in flight: the summary shows "running…" and
// the previous results stay visible until the new ones land.
func (m *Model) StartRun(configName, dir string) {
	m.configName = configName
	m.dir = dir
	m.running = true
	m.hasCoverage = false
}

// SetCoverage stamps the run-wide coverage percentage onto the summary
// (#2081); StartRun resets it, so only a coverage run shows one.
func (m *Model) SetCoverage(pct float64) {
	m.coverage = pct
	m.hasCoverage = true
}

// FinishRun installs a completed run's parsed results and raw output and
// rebuilds the tree. results may be nil (no parser output — a crashed tool,
// a language whose parser bailed): the raw output remains readable via 'o'.
func (m *Model) FinishRun(results []lang.TestResult, raw string) {
	m.running = false
	m.results = results
	m.raw = raw
	m.rawMode = results == nil
	m.detailTop = 0
	m.rebuild()
}

// FailedIDs returns the unique RerunIDs of the failed results, in first-
// failure order — the re-run-failed target set.
func (m *Model) FailedIDs() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range m.results {
		if r.Status != lang.TestFail || r.RerunID == "" || seen[r.RerunID] {
			continue
		}
		seen[r.RerunID] = true
		out = append(out, r.RerunID)
	}
	return out
}

// rebuild derives the row tree from the results, keeping the cursor on the
// same node label path where possible, and re-memoizes the summary counts.
func (m *Model) rebuild() {
	keep := ""
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		keep = nodePath(m.rows[m.cursor].n)
	}
	root := &node{}
	index := map[string]*node{}
	for i := range m.results {
		r := &m.results[i]
		segs := append([]string{r.Group}, strings.Split(r.Name, "/")...)
		path, parent := "", root
		for j, seg := range segs {
			path += "/" + seg
			child, ok := index[path]
			if !ok {
				child = &node{label: seg, depth: j}
				index[path] = child
				parent.children = append(parent.children, child)
			}
			parent = child
			if j == len(segs)-1 {
				child.res = r
			}
		}
	}
	m.rows = nil
	var flatten func(*node)
	flatten = func(n *node) {
		for _, c := range n.children {
			if n != root {
				c.up = n
			}
			m.rows = append(m.rows, row{n: c})
			flatten(c)
		}
	}
	flatten(root)
	m.passed, m.failed, m.skipped, m.elapsed = 0, 0, 0, 0
	for _, r := range m.results {
		switch r.Status {
		case lang.TestPass:
			m.passed++
		case lang.TestFail:
			m.failed++
		case lang.TestSkip:
			m.skipped++
		}
		m.elapsed += r.Elapsed
	}
	m.cursor = 0
	for i, r := range m.rows {
		if nodePath(r.n) == keep {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

// nodePath is the node's stable identity for cursor preservation.
func nodePath(n *node) string {
	if n == nil {
		return ""
	}
	var segs []string
	for c := n; c != nil && c.label != ""; c = c.up {
		segs = append([]string{c.label}, segs...)
	}
	return strings.Join(segs, "/")
}

// status is the node's own or aggregated outcome: a synthesized parent fails
// when any descendant fails, skips when every descendant skips.
func (n *node) status() lang.TestStatus {
	if n.res != nil {
		return n.res.Status
	}
	allSkip := true
	anyFail := false
	for _, c := range n.children {
		switch c.status() {
		case lang.TestFail:
			anyFail = true
			allSkip = false
		case lang.TestPass:
			allSkip = false
		}
	}
	if anyFail {
		return lang.TestFail
	}
	if allSkip && len(n.children) > 0 {
		return lang.TestSkip
	}
	return lang.TestPass
}

// Update handles one message while the panel exists; only key presses reach
// it, focus-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return m.handleKey(k)
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "tab", "l", "right":
		m.detailFocus = true
		return nil
	case "h", "left":
		m.detailFocus = false
		return nil
	case "o":
		m.rawMode = !m.rawMode
		m.detailTop = 0
		return nil
	case "r":
		return emit(RerunMsg{})
	case "f":
		if len(m.FailedIDs()) > 0 {
			return emit(RerunMsg{Failed: true})
		}
		return nil
	case "t":
		if r := m.selected(); r != nil && r.RerunID != "" {
			return emit(RerunMsg{ID: r.RerunID})
		}
		return nil
	case "y":
		// vim's yank on the marked row (#2071); the tree row is the copy
		// target in both columns — the detail text scrolls, it is not marked.
		return m.copyRow(m.cursor)
	}
	if ui.CopyChord(msg.String()) {
		return m.copyRow(m.cursor)
	}
	if m.detailFocus {
		m.detailKey(msg.String())
		return nil
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(msg.String(), &m.cursor, len(m.rows), m.bodyHeight(), ui.NavFull) {
		m.detailTop = 0
		m.clampScroll()
		return nil
	}
	if msg.String() == "enter" {
		return m.activate(m.cursor)
	}
	m.clampScroll()
	return nil
}

// detailKey scrolls the detail column.
func (m *Model) detailKey(key string) {
	switch key {
	case "j", "down":
		m.detailTop++
	case "k", "up":
		m.detailTop--
	case "pgdown", "ctrl+d":
		m.detailTop += m.bodyHeight()
	case "pgup", "ctrl+u":
		m.detailTop -= m.bodyHeight()
	case "g", "home":
		m.detailTop = 0
	case "G", "end":
		m.detailTop = len(m.detailLines()) - m.bodyHeight()
	}
	m.clampDetail()
}

// selected returns the result under the cursor, nil for synthesized nodes.
func (m *Model) selected() *lang.TestResult {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].n.res
}

// activate jumps to the row's test: the failure location when the parser
// found one (resolved against the run's working directory), the test's
// source declaration otherwise.
func (m *Model) activate(i int) tea.Cmd {
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	n := m.rows[i].n
	r := n.res
	if r == nil {
		// A container opens its first failing descendant — preferring one
		// with a parsed failure location — else its first descendant.
		var firstFail *lang.TestResult
		for j := i + 1; j < len(m.rows) && m.rows[j].n.depth > n.depth; j++ {
			c := m.rows[j].n.res
			if c == nil {
				continue
			}
			if r == nil {
				r = c
			}
			if c.Status == lang.TestFail {
				if firstFail == nil {
					firstFail = c
				}
				if c.File != "" && c.Line > 0 {
					firstFail = c
					break
				}
			}
		}
		if firstFail != nil {
			r = firstFail
		}
		if r == nil {
			return nil
		}
	}
	if r.File != "" && r.Line > 0 {
		path := r.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(m.dir, path)
		}
		return emit(OpenLocationMsg{Path: path, Line: r.Line - 1})
	}
	if r.RerunID != "" {
		return emit(LocateTestMsg{RerunID: r.RerunID})
	}
	return nil
}

// copyRow puts the marked row on the clipboard through a CopyMsg (#2071):
// the outcome, the test's full tree path, its duration and — when the parser
// found one — the failure location. An empty tree copies nothing.
func (m *Model) copyRow(i int) tea.Cmd {
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	n := m.rows[i].n
	text := statusWord(n.status()) + " " + nodePath(n)
	if r := n.res; r != nil {
		if r.Elapsed > 0 {
			text += "  " + formatSecs(r.Elapsed)
		}
		if r.File != "" && r.Line > 0 {
			path := r.File
			if !filepath.IsAbs(path) {
				path = filepath.Join(m.dir, path)
			}
			text += "  " + path + ":" + strconv.Itoa(r.Line)
		}
	}
	return emit(CopyMsg{Text: text, What: "test result"})
}

// statusWord names an outcome in copied text, where glyphs read poorly.
func statusWord(s lang.TestStatus) string {
	switch s {
	case lang.TestFail:
		return "FAIL"
	case lang.TestSkip:
		return "SKIP"
	}
	return "PASS"
}

// emit wraps a message into the deferred command shape the pane layer expects.
func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// View renders the summary header, the tree and detail columns, and the
// key-hint footer.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	tw, dw := m.colWidths()
	tree := strings.Split(m.renderTree(pal, m.bodyHeight(), tw), "\n")
	detail := m.renderDetail(pal, m.bodyHeight(), dw)
	sep := lipgloss.NewStyle().Foreground(pal.Border).Render("│")
	for i := 0; i < m.bodyHeight(); i++ {
		b.WriteString(pad(lineAt(tree, i), tw) + sep + lineAt(detail, i))
		b.WriteString("\n")
	}
	b.WriteString(m.footer(pal))
	return b.String()
}

// headerLine is the run summary: config name, counts, total time.
func (m *Model) headerLine(pal *theme.Palette) string {
	title := " Test Results"
	if m.configName != "" {
		title += " — " + m.configName
	}
	var state string
	switch {
	case m.running:
		state = "running…"
	case len(m.results) == 0 && m.raw != "":
		state = "no structured results (o raw output)"
	case len(m.results) == 0:
		state = "no tests run yet"
	default:
		parts := []string{plural(m.passed, "passed"), plural(m.failed, "failed")}
		if m.skipped > 0 {
			parts = append(parts, plural(m.skipped, "skipped"))
		}
		state = strings.Join(parts, " · ")
		if m.elapsed > 0 {
			state += " · " + formatSecs(m.elapsed)
		}
		if m.hasCoverage {
			state += " · " + strconv.FormatFloat(m.coverage, 'f', 1, 64) + "% coverage"
		}
	}
	head := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(title)
	return head + lipgloss.NewStyle().Faint(true).Render("   "+state)
}

// plural renders "1 passed" — the unit is already plural-neutral.
func plural(n int, unit string) string {
	return strconv.Itoa(n) + " " + unit
}

// formatSecs renders a duration in seconds compactly ("12ms", "1.24s").
func formatSecs(s float64) string {
	if s < 1 {
		return strconv.Itoa(int(s*1000)) + "ms"
	}
	return strconv.FormatFloat(s, 'f', 2, 64) + "s"
}

// renderTree draws the scrolled result tree into the left column.
func (m *Model) renderTree(pal *theme.Palette, height, width int) string {
	if len(m.rows) == 0 {
		text := "(no results)"
		if m.running {
			text = "(running…)"
		}
		return lipgloss.NewStyle().Faint(true).Render(" "+text) + strings.Repeat("\n", height)
	}
	m.clampScroll()
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.top + k
		if i < len(m.rows) {
			b.WriteString(m.renderRow(pal, i, width))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderRow draws one tree line: indent, status glyph, label, duration.
func (m *Model) renderRow(pal *theme.Palette, i, width int) string {
	n := m.rows[i].n
	st := n.status()
	line := " " + strings.Repeat("  ", n.depth) + statusGlyph(st) + " " + n.label
	if n.res != nil && n.res.Elapsed > 0 {
		line += "  " + formatSecs(n.res.Elapsed)
	}
	style := lipgloss.NewStyle().Foreground(statusColor(pal, st))
	if n.res == nil {
		style = style.Bold(true)
	}
	if i == m.cursor {
		if m.focused && !m.detailFocus {
			style = style.Background(pal.Selection).Bold(true)
		} else {
			// Muted cursor row while unfocused (#1034), like the siblings.
			style = style.Background(pal.SelectionMuted)
		}
	}
	return style.Render(clipTo(line, width))
}

// detailLines is the detail column's content: the raw run output in raw
// mode, the selected test's buffered output otherwise.
func (m *Model) detailLines() []string {
	if m.rawMode {
		return strings.Split(strings.TrimRight(m.raw, "\n"), "\n")
	}
	if r := m.selected(); r != nil && r.Output != "" {
		return strings.Split(strings.TrimRight(r.Output, "\n"), "\n")
	}
	return nil
}

// renderDetail draws the scrolled detail column.
func (m *Model) renderDetail(pal *theme.Palette, height, width int) []string {
	lines := m.detailLines()
	out := make([]string, 0, height)
	if len(lines) == 0 {
		hint := "(no output)"
		if !m.rawMode {
			hint = "(no output — o for raw run output)"
		}
		out = append(out, lipgloss.NewStyle().Faint(true).Render(clipTo(" "+hint, width)))
		return out
	}
	m.clampDetail()
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	for k := 0; k < height; k++ {
		i := m.detailTop + k
		if i >= len(lines) {
			break
		}
		out = append(out, style.Render(clipTo(" "+strings.ReplaceAll(lines[i], "\t", "    "), width)))
	}
	return out
}

// footer shows the key hints, naming the re-run actions (#1911).
func (m *Model) footer(pal *theme.Palette) string {
	mode := "test output"
	if m.rawMode {
		mode = "raw"
	}
	return lipgloss.NewStyle().Faint(true).Render(clipTo(" enter open · y copy · r rerun · f rerun failed · t rerun test · o "+mode+" · tab pane", m.width))
}

// statusGlyph maps an outcome to its marker.
func statusGlyph(s lang.TestStatus) string {
	switch s {
	case lang.TestFail:
		return "✗"
	case lang.TestSkip:
		return "○"
	}
	return "✓"
}

// statusColor maps an outcome to the theme's slots.
func statusColor(pal *theme.Palette, s lang.TestStatus) color.Color {
	switch s {
	case lang.TestFail:
		return pal.Error
	case lang.TestSkip:
		return pal.Warning
	}
	return pal.Success
}

// colWidths splits the interior width between tree and detail columns.
func (m *Model) colWidths() (tree, detail int) {
	w := m.width - 1 // separator column
	if w < 2 {
		return 1, 1
	}
	tree = w / 2
	if tree < 12 {
		tree = 12
	}
	if tree > w-8 {
		tree = w - 8
	}
	if tree < 1 {
		tree = 1
	}
	return tree, w - tree
}

// lineAt returns lines[i] or "".
func lineAt(lines []string, i int) string {
	if i >= 0 && i < len(lines) {
		return lines[i]
	}
	return ""
}

// pad right-pads the rendered line to width columns.
func pad(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// bodyHeight is the room between the header and footer lines.
func (m *Model) bodyHeight() int {
	h := m.height - 2
	if h < 1 {
		h = 1
	}
	return h
}

// clampScroll keeps the cursor valid and inside the visible window.
func (m *Model) clampScroll() {
	if m.cursor > len(m.rows)-1 {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.top > m.cursor {
		m.top = m.cursor
	}
	if h := m.bodyHeight(); m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

// clampDetail keeps the detail scroll inside its content.
func (m *Model) clampDetail() {
	max := len(m.detailLines()) - m.bodyHeight()
	if m.detailTop > max {
		m.detailTop = max
	}
	if m.detailTop < 0 {
		m.detailTop = 0
	}
}

// clipTo bounds one rendered line to width columns.
func clipTo(s string, width int) string {
	if width > 0 && len([]rune(s)) > width {
		return string([]rune(s)[:width-1]) + "…"
	}
	return s
}

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

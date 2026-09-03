// Package depspanel is the Dependencies tool window (#2419): a singleton
// bottom-split pane listing the project's declared dependencies per
// manifest file — name, current, latest, direct/indirect and the
// vulnerability count — the Problems pane's shape applied to internal/deps
// scan results. The pane is a pure consumer: the root model runs scans in
// the background and hands the finished deps.Result over via Set; nothing
// here touches the toolchain. "/" opens the shared list-filter row
// (#2156): manifest:, state: (outdated|vulnerable|fresh) plus free match
// text over the dependency name.
package depspanel

import (
	"path/filepath"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/deps"
	"ike/internal/filterbar"
	"ike/internal/filterexpr"
	"ike/internal/theme"
	"ike/internal/ui"
)

// OpenLocationMsg asks the root model to open the manifest at the
// dependency's declaration line (0-based), the navigation seam the
// Problems pane uses.
type OpenLocationMsg struct {
	Path string
	Line int
}

// BumpMsg asks the root model to update the marked dependency to Latest:
// rewrite the manifest via the provider's Bump, then offer the install
// step behind a confirmation (#2419 — never run installs unasked).
type BumpMsg struct {
	Path     string
	Provider string
	Dep      deps.Dep
}

// VulnsMsg asks the root model to show the marked dependency's
// vulnerability details in the centered dialog.
type VulnsMsg struct {
	Dep deps.Dep
}

// RefreshMsg asks the root model to force a rescan ('r', deps.refresh).
type RefreshMsg struct{}

// row is one rendered line: a manifest header or one dependency under it.
type row struct {
	header   bool
	manifest deps.ManifestDeps
	d        deps.Dep
}

// States is the state gate's vocabulary.
var States = []string{"outdated", "vulnerable", "fresh"}

// Schema is the pane's filter language.
var Schema = filterexpr.Schema{Fields: []filterexpr.Field{
	{Name: "manifest", ValueDoc: "a manifest path or substring", Doc: "manifest file"},
	{Name: "state", Values: States, Doc: "update/vulnerability state, repeatable (OR)"},
}}

// Model is the tool window state. Value type with pointer-receiver
// mutators, embedded in a pane.Instance like the Problems pane.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	result      deps.Result
	scanning    bool
	displayPath func(string) string

	filter filterbar.Model
	rows   []row
	cursor int
	top    int

	clicks ui.ClickTracker
	now    func() time.Time
}

// New returns an empty panel; results arrive via Set.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, now: time.Now, filter: filterbar.New(Schema)}
}

// Set replaces the scan result and rebuilds the rows.
func (m *Model) Set(r deps.Result) {
	m.result = r
	m.Refresh()
}

// SetScanning flags an in-flight background scan for the header.
func (m *Model) SetScanning(s bool) { m.scanning = s }

// SetDisplayPath injects the project-relative path shortener.
func (m *Model) SetDisplayPath(f func(string) string) { m.displayPath = f }

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

// Refresh re-derives the rows from the result, keeping the cursor on the
// same dependency (manifest + name) where possible.
func (m *Model) Refresh() {
	keepPath, keepName, keepHeader := "", "", false
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		r := m.rows[m.cursor]
		keepPath, keepName, keepHeader = r.manifest.Path, r.d.Name, r.header
	}
	m.rows = nil
	for _, md := range m.result.Manifests {
		var kept []deps.Dep
		for _, d := range md.Deps {
			if m.matches(md, d) {
				kept = append(kept, d)
			}
		}
		if len(kept) == 0 {
			continue
		}
		m.rows = append(m.rows, row{header: true, manifest: md})
		for _, d := range kept {
			m.rows = append(m.rows, row{manifest: md, d: d})
		}
	}
	m.cursor = 0
	for i, r := range m.rows {
		if r.manifest.Path == keepPath && r.header == keepHeader && (r.header || r.d.Name == keepName) {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

// matches gates one dependency through the filter.
func (m *Model) matches(md deps.ManifestDeps, d deps.Dep) bool {
	q := m.filter.Query()
	if q.Empty() {
		return true
	}
	if manifests := q.Values("manifest"); len(manifests) > 0 && !m.pathMatches(manifests, md.Path) {
		return false
	}
	if states := q.Values("state"); len(states) > 0 && !hasString(states, depState(d)) {
		return false
	}
	if _, ok := filterexpr.MatchText(q.Match, d.Name); !ok {
		return false
	}
	return true
}

// depState classifies a dependency for the state: gate; vulnerable wins
// over outdated because it is what the filter user is hunting.
func depState(d deps.Dep) string {
	if len(d.Vulns) > 0 {
		return "vulnerable"
	}
	if d.Outdated() {
		return "outdated"
	}
	return "fresh"
}

// pathMatches applies the manifest: values, OR'd, against the rendered
// and the raw path.
func (m *Model) pathMatches(pats []string, path string) bool {
	shown := m.shorten(path)
	for _, p := range pats {
		if filterexpr.MatchPath(p, shown) || filterexpr.MatchPath(p, path) {
			return true
		}
	}
	return false
}

func hasString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// Update handles one message while the panel exists; only key presses
// reach it, focus-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		return m.handleKey(k)
	}
	return nil
}

// Filtering reports whether the filter row holds the keyboard (#2409).
func (m *Model) Filtering() bool { return m.filter.Active() }

// OpenSearch implements the pane's Searchable capability (#2409): the
// shared find chord focuses the filter row, exactly as "/" does.
func (m *Model) OpenSearch() bool {
	m.filter.Focus()
	return true
}

// NextMatch implements the pane's match-step capability (#2410), walking
// the dependency rows the filter left standing; headers are chrome.
func (m *Model) NextMatch() ui.MatchStep { return m.stepFiltered(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.stepFiltered(-1) }

func (m *Model) stepFiltered(delta int) ui.MatchStep {
	return ui.StepFiltered(&m.filter, &m.cursor, &m.top, len(m.rows), m.bodyHeight(), delta,
		func(i int) bool { return !m.rows[i].header })
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
	case "enter":
		return m.activate(m.cursor)
	case "u":
		// Apply the update: rewrite the manifest, then the app offers
		// the install step behind a confirmation.
		return m.bump(m.cursor)
	case "v":
		return m.showVulns(m.cursor)
	case "r":
		return func() tea.Msg { return RefreshMsg{} }
	}
	m.clampScroll()
	return nil
}

// targetRow resolves row i to the dependency an action acts on: a header
// stands for its first dependency, everything else for itself.
func (m *Model) targetRow(i int) (row, bool) {
	return ui.TargetRow(m.rows, i, func(r row) bool { return r.header })
}

// activate opens the manifest at the marked dependency's line; a header
// opens the manifest at the top.
func (m *Model) activate(i int) tea.Cmd {
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	r := m.rows[i]
	msg := OpenLocationMsg{Path: r.manifest.Path}
	if !r.header && r.d.Line > 0 {
		msg.Line = r.d.Line - 1
	}
	return func() tea.Msg { return msg }
}

// bump asks the app to update the marked dependency; up-to-date rows and
// headers ask for nothing.
func (m *Model) bump(i int) tea.Cmd {
	r, ok := m.targetRow(i)
	if !ok || !r.d.Outdated() {
		return nil
	}
	msg := BumpMsg{Path: r.manifest.Path, Provider: r.manifest.Provider, Dep: r.d}
	return func() tea.Msg { return msg }
}

// showVulns asks for the marked dependency's vulnerability details; clean
// rows ask for nothing.
func (m *Model) showVulns(i int) tea.Cmd {
	r, ok := m.targetRow(i)
	if !ok || len(r.d.Vulns) == 0 {
		return nil
	}
	msg := VulnsMsg{Dep: r.d}
	return func() tea.Msg { return msg }
}

// Selected resolves the marked row for the app-side commands (tests).
func (m *Model) Selected() (deps.ManifestDeps, deps.Dep, bool) {
	r, ok := m.targetRow(m.cursor)
	if !ok {
		return deps.ManifestDeps{}, deps.Dep{}, false
	}
	return r.manifest, r.d, true
}

// View renders the header, the filter row, the scrolled rows and the
// key-hint footer.
func (m *Model) View() string {
	pal := m.theme()
	return ui.ListPaneView(m.headerLine(pal), m.filter.View(m.width, pal), m.renderRows(pal, m.bodyHeight()), m.footer(pal))
}

// headerLine names the pane and totals the outdated/vulnerable counts.
func (m *Model) headerLine(pal *theme.Palette) string {
	counts := strconv.Itoa(m.result.OutdatedCount()) + " outdated · " +
		strconv.Itoa(m.result.VulnCount()) + " vulnerabilities"
	if m.scanning {
		counts = "scanning… · " + counts
	}
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" Dependencies")
	return title + lipgloss.NewStyle().Faint(true).Render("   "+counts)
}

// renderRows draws the flattened list scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	m.clampScroll()
	empty := lipgloss.NewStyle().Faint(true).Render(" " + m.emptyText())
	return ui.RenderWindow(m.top, height, len(m.rows), empty, func(i int) string { return m.renderRow(pal, i) })
}

// emptyText explains an empty list per scan state and filter.
func (m *Model) emptyText() string {
	if m.scanning {
		return "(scanning…)"
	}
	if len(m.result.Manifests) > 0 && !m.filter.Empty() {
		return "(no dependencies match the filter)"
	}
	return "(no dependency manifests found in the project root)"
}

// renderRow draws one line: manifest headers accented, dependencies as
// "name  current → latest  kind  vulns".
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	r := m.rows[i]
	var line string
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	if r.header {
		line = " " + m.shorten(r.manifest.Path) + "  (" + r.manifest.Provider + ")"
		style = lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	} else {
		line = "   " + stateGlyph(r.d) + " " + r.d.Name + "  " + versionCell(r.d) + "  " + kindCell(r.d)
		if n := len(r.d.Vulns); n > 0 {
			line += "  " + strconv.Itoa(n) + " vuln"
			if n != 1 {
				line += "s"
			}
			style = style.Foreground(pal.Warning)
		} else if r.d.Outdated() {
			style = style.Foreground(pal.Info)
		}
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

// stateGlyph marks a row: vulnerable, outdated or fresh.
func stateGlyph(d deps.Dep) string {
	if len(d.Vulns) > 0 {
		return "▲"
	}
	if d.Outdated() {
		return "↑"
	}
	return "·"
}

// versionCell renders "current → latest" or just the current version.
func versionCell(d deps.Dep) string {
	cur := d.Current
	if cur == "" {
		cur = "?"
	}
	if d.Outdated() {
		return cur + " → " + d.Latest
	}
	return cur
}

// kindCell renders the direct/indirect column.
func kindCell(d deps.Dep) string {
	if d.Indirect {
		return "indirect"
	}
	return "direct"
}

// footer shows the key hints.
func (m *Model) footer(pal *theme.Palette) string {
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter open · u update · v vulns · r rescan · / filter · j/k move"))
}

// shorten renders a path project-relative when the app injected a
// shortener; the fallback keeps at least the base name readable.
func (m *Model) shorten(path string) string {
	if m.displayPath != nil {
		return m.displayPath(path)
	}
	return filepath.Base(path)
}

// bodyHeight is the room the list gets between header, filter and footer.
func (m *Model) bodyHeight() int {
	h := m.height - 3
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

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// Mouse control, mirroring the Problems pane: y 0 is the header, y 1 the
// filter row, list rows start at y 2.
const headerRows = 2

// Wheel scrolls the list by delta rows (positive = down).
func (m *Model) Wheel(delta int) {
	ui.WheelWindow(&m.top, &m.cursor, delta, len(m.rows), m.bodyHeight())
}

// Click selects a row; a second click within the double-click window
// activates it.
func (m *Model) Click(x, y int) tea.Cmd {
	return m.clicks.ClickRow(y, m.top, headerRows, m.bodyHeight(), len(m.rows), m.now(), &m.cursor, m.activate)
}

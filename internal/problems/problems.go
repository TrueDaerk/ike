// Package problems is the Problems tool window (#1024, part of #33): a
// singleton bottom-split pane aggregating the current LSP diagnostics
// project-wide, JetBrains' Problems view scaled to the terminal. It is a pure
// consumer of the existing publishDiagnostics flow — the root model feeds a
// shared Store from every DiagnosticsMsg and the panel re-derives its rows;
// no new LSP traffic ever originates here.
package problems

import (
	"image/color"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/theme"
)

// OpenLocationMsg asks the root model to open Path and place the cursor at
// the 0-based Line/Col — the same navigation seam go-to-definition uses.
type OpenLocationMsg struct {
	Path string
	Line int
	Col  int
}

// Store is the app-level per-file diagnostics store: the latest published set
// for every path any language server reported on, opened in an editor or not.
// The root model owns one instance for the whole session and replaces a
// path's set wholesale on every publish, mirroring the editor's own cache.
type Store struct {
	byPath map[string][]ilsp.Diagnostic
}

// NewStore returns an empty store.
func NewStore() *Store { return &Store{byPath: map[string][]ilsp.Diagnostic{}} }

// Set replaces path's diagnostic set; an empty set removes the path so a
// fixed file drops out of the pane entirely.
func (s *Store) Set(path string, diags []ilsp.Diagnostic) {
	if len(diags) == 0 {
		delete(s.byPath, path)
		return
	}
	s.byPath[path] = diags
}

// Get returns path's current diagnostic set (nil when clean).
func (s *Store) Get(path string) []ilsp.Diagnostic { return s.byPath[path] }

// Paths returns every path holding diagnostics, sorted lexicographically.
func (s *Store) Paths() []string {
	out := make([]string, 0, len(s.byPath))
	for p := range s.byPath {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Len reports how many files currently hold diagnostics.
func (s *Store) Len() int { return len(s.byPath) }

// row is one rendered line: a file header or one diagnostic under it.
type row struct {
	header bool
	path   string
	d      ilsp.Diagnostic
}

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the VCS panel.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	store       *Store
	displayPath func(string) string

	// fileOnly scopes the list to the active editor's file ('f' toggles).
	fileOnly   bool
	activePath string

	rows   []row
	cursor int
	top    int

	// Double-click detection mirrors the VCS panel (#514): activating a row
	// needs a second click on the same row within doubleClickWindow; now is
	// injectable so tests control the clock.
	lastClickRow int
	lastClickAt  time.Time
	now          func() time.Time
}

// New returns an empty panel; the store arrives via SetStore.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, lastClickRow: -1, now: time.Now}
}

// SetStore shares the app-level diagnostics store and rebuilds the rows.
func (m *Model) SetStore(s *Store) {
	m.store = s
	m.Refresh()
}

// SetDisplayPath injects the project-relative path shortener the app already
// uses for the finder; unset falls back to the raw (absolute) path.
func (m *Model) SetDisplayPath(f func(string) string) { m.displayPath = f }

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the panel focused (header + selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// FileOnly reports the scope filter state (tests, persistence).
func (m *Model) FileOnly() bool { return m.fileOnly }

// SetActivePath tracks the focused editor's file for the current-file scope;
// the root model calls it on focus and tab changes.
func (m *Model) SetActivePath(path string) {
	if path == m.activePath {
		return
	}
	m.activePath = path
	if m.fileOnly {
		m.Refresh()
	}
}

// Rows reports the flattened row count (tests).
func (m *Model) Rows() int { return len(m.rows) }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// Refresh re-derives the rows from the store, keeping the cursor on the same
// diagnostic (path + position) where possible. The root model calls it after
// every store update so the pane tracks publishes live.
func (m *Model) Refresh() {
	keepPath, keepLine, keepCol, keepHeader := "", -1, -1, false
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		r := m.rows[m.cursor]
		keepPath, keepHeader = r.path, r.header
		keepLine, keepCol = r.d.Range.Start.Line, r.d.Range.Start.Col
	}
	m.rows = nil
	if m.store != nil {
		for _, path := range m.sortedPaths() {
			m.rows = append(m.rows, row{header: true, path: path})
			diags := append([]ilsp.Diagnostic(nil), m.store.Get(path)...)
			sort.SliceStable(diags, func(i, j int) bool {
				a, b := diags[i], diags[j]
				if sa, sb := normSev(a.Severity), normSev(b.Severity); sa != sb {
					return sa < sb
				}
				if a.Range.Start.Line != b.Range.Start.Line {
					return a.Range.Start.Line < b.Range.Start.Line
				}
				return a.Range.Start.Col < b.Range.Start.Col
			})
			for _, d := range diags {
				m.rows = append(m.rows, row{path: path, d: d})
			}
		}
	}
	m.cursor = 0
	for i, r := range m.rows {
		if r.path == keepPath && r.header == keepHeader &&
			(r.header || (r.d.Range.Start.Line == keepLine && r.d.Range.Start.Col == keepCol)) {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

// sortedPaths orders the visible files: worst severity first, then path —
// files with errors surface above warning-only files. The current-file scope
// reduces the list to the active editor's path.
func (m *Model) sortedPaths() []string {
	var paths []string
	for _, p := range m.store.Paths() {
		if m.fileOnly && p != m.activePath {
			continue
		}
		paths = append(paths, p)
	}
	sort.SliceStable(paths, func(i, j int) bool {
		wi, wj := worstSev(m.store.Get(paths[i])), worstSev(m.store.Get(paths[j]))
		if wi != wj {
			return wi < wj
		}
		return paths[i] < paths[j]
	})
	return paths
}

// normSev maps an unspecified severity to error, matching the gutter.
func normSev(sev int) int {
	if sev < 1 || sev > 4 {
		return 1
	}
	return sev
}

// worstSev returns the most severe (lowest) normalized severity in diags.
func worstSev(diags []ilsp.Diagnostic) int {
	worst := 5
	for _, d := range diags {
		if s := normSev(d.Severity); s < worst {
			worst = s
		}
	}
	return worst
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
	case "j", "down":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(0, len(m.rows)-1)
	case "f":
		m.fileOnly = !m.fileOnly
		m.Refresh()
	case "enter":
		return m.activate(m.cursor)
	}
	m.clampScroll()
	return nil
}

// activate opens the diagnostic under row i; a file header opens the file at
// its first (most severe) diagnostic.
func (m *Model) activate(i int) tea.Cmd {
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	r := m.rows[i]
	if r.header {
		if i+1 < len(m.rows) && !m.rows[i+1].header {
			r = m.rows[i+1]
		} else {
			return nil
		}
	}
	msg := OpenLocationMsg{Path: r.path, Line: r.d.Range.Start.Line, Col: r.d.Range.Start.Col}
	return func() tea.Msg { return msg }
}

// View renders the scope header, the scrolled rows, and the key-hint footer.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	b.WriteString(m.renderRows(pal, m.bodyHeight()))
	b.WriteString(m.footer(pal))
	return b.String()
}

// headerLine names the scope and totals the visible problems.
func (m *Model) headerLine(pal *theme.Palette) string {
	scope := "project"
	if m.fileOnly {
		scope = "current file"
	}
	errs, warns := m.visibleCounts()
	counts := strconv.Itoa(errs) + " errors · " + strconv.Itoa(warns) + " warnings"
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" Problems — " + scope)
	return title + lipgloss.NewStyle().Faint(true).Render("   "+counts)
}

// visibleCounts totals errors and warnings across the listed rows.
func (m *Model) visibleCounts() (errs, warns int) {
	for _, r := range m.rows {
		if r.header {
			continue
		}
		switch normSev(r.d.Severity) {
		case 1:
			errs++
		case 2:
			warns++
		}
	}
	return errs, warns
}

// renderRows draws the flattened list scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	if len(m.rows) == 0 {
		return lipgloss.NewStyle().Faint(true).Render(" "+m.emptyText()) + strings.Repeat("\n", height)
	}
	m.clampScroll()
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.top + k
		if i < len(m.rows) {
			b.WriteString(m.renderRow(pal, i))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// emptyText explains an empty list per scope.
func (m *Model) emptyText() string {
	if m.fileOnly {
		if m.activePath == "" {
			return "(no active file — press f for the whole project)"
		}
		return "(no problems in the current file)"
	}
	return "(no problems)"
}

// renderRow draws one line: file headers accented, diagnostics glyph-tagged.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	r := m.rows[i]
	var line string
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	if r.header {
		line = " " + m.shorten(r.path)
		style = lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	} else {
		pos := strconv.Itoa(r.d.Range.Start.Line+1) + ":" + strconv.Itoa(r.d.Range.Start.Col+1)
		msg := r.d.Message
		if n := strings.IndexByte(msg, '\n'); n >= 0 {
			msg = msg[:n]
		}
		line = "   " + sevGlyph(r.d.Severity) + " " + pos + "  " + msg
		if r.d.Code != "" {
			line += " (" + r.d.Code + ")"
		}
		style = style.Foreground(sevColor(pal, r.d.Severity))
	}
	if i == m.cursor {
		if m.focused {
			style = style.Background(pal.Selection).Bold(true)
		} else {
			// Muted cursor row while unfocused (#1034), like the siblings.
			style = style.Background(pal.SelectionMuted)
		}
	}
	return style.Render(m.clip(line))
}

// footer shows the key hints, naming the scope toggle (#1024).
func (m *Model) footer(pal *theme.Palette) string {
	scope := "current file"
	if m.fileOnly {
		scope = "project"
	}
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter open · f " + scope + " · j/k move"))
}

// sevGlyph maps a severity to its marker, unspecified counting as error.
func sevGlyph(sev int) string {
	switch normSev(sev) {
	case 2:
		return "▲"
	case 3:
		return "ℹ"
	case 4:
		return "✦"
	}
	return "●"
}

// sevColor maps a severity to the theme's diagnostic slots.
func sevColor(pal *theme.Palette, sev int) color.Color {
	switch normSev(sev) {
	case 2:
		return pal.Warning
	case 3:
		return pal.Info
	case 4:
		return pal.Hint
	}
	return pal.Error
}

// shorten renders a path project-relative when the app injected a shortener.
func (m *Model) shorten(path string) string {
	if m.displayPath != nil {
		return m.displayPath(path)
	}
	return path
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

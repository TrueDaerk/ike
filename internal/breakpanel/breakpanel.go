// Package breakpanel is the Breakpoints tool window (#1377): a singleton
// bottom-split pane listing every breakpoint in the project grouped by file,
// JetBrains' Breakpoints dialog scaled to the terminal. It is a pure consumer
// of the app-owned breakpoint store (#577) — the root model owns mutation and
// persistence; the panel only renders the live set and emits messages for
// jump, enable/disable, delete and delete-all, so the gutter and the list can
// never disagree.
package breakpanel

import (
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/debug"
	"ike/internal/theme"
)

// OpenLocationMsg asks the root model to open Path (a store key, project-
// relative) and place the cursor on the 0-based Line — the same navigation
// seam the Problems pane uses.
type OpenLocationMsg struct {
	Path string
	Line int
}

// ToggleEnabledMsg asks the root model to flip the enabled state of the
// breakpoint on Path:Line (#1377).
type ToggleEnabledMsg struct {
	Path string
	Line int
}

// RemoveMsg asks the root model to delete the breakpoint on Path:Line.
type RemoveMsg struct {
	Path string
	Line int
}

// RemoveAllMsg asks the root model to delete every breakpoint.
type RemoveAllMsg struct{}

// row is one rendered line: a file header or one breakpoint under it.
type row struct {
	header  bool
	path    string
	line    int
	enabled bool
	preview string
}

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the Problems panel.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	store       *debug.Breakpoints
	displayPath func(string) string
	preview     func(path string, line int) string

	rows   []row
	cursor int
	top    int

	// Double-click detection mirrors the Problems panel: activating a row
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

// SetStore shares the app-level breakpoint store and rebuilds the rows.
func (m *Model) SetStore(s *debug.Breakpoints) {
	m.store = s
	m.Refresh()
}

// SetDisplayPath injects the project-relative path shortener; unset falls
// back to the raw store key.
func (m *Model) SetDisplayPath(f func(string) string) { m.displayPath = f }

// SetPreview injects the source-line reader for the row previews; unset
// leaves the preview column empty.
func (m *Model) SetPreview(f func(path string, line int) string) { m.preview = f }

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

// Refresh re-derives the rows from the store, keeping the cursor on the same
// breakpoint (path + line) where possible. The root model calls it after
// every store mutation — gutter toggles included — so the list stays live.
func (m *Model) Refresh() {
	keepPath, keepLine, keepHeader := "", -1, false
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		r := m.rows[m.cursor]
		keepPath, keepLine, keepHeader = r.path, r.line, r.header
	}
	m.rows = nil
	if m.store != nil {
		for _, path := range sortedKeys(m.store.All()) {
			m.rows = append(m.rows, row{header: true, path: path})
			for _, l := range m.store.Lines(path) {
				r := row{path: path, line: l, enabled: m.store.Enabled(path, l)}
				if m.preview != nil {
					r.preview = m.preview(path, l)
				}
				m.rows = append(m.rows, r)
			}
		}
	}
	m.cursor = 0
	for i, r := range m.rows {
		if r.path == keepPath && r.header == keepHeader && (r.header || r.line == keepLine) {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

// sortedKeys orders the store's files lexicographically.
func sortedKeys(files map[string][]int) []string {
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
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
	case "enter":
		return m.activate(m.cursor)
	case "space":
		if r, ok := m.selected(); ok {
			msg := ToggleEnabledMsg{Path: r.path, Line: r.line}
			return func() tea.Msg { return msg }
		}
	case "d", "delete", "backspace":
		if r, ok := m.selected(); ok {
			msg := RemoveMsg{Path: r.path, Line: r.line}
			return func() tea.Msg { return msg }
		}
	case "D":
		if len(m.rows) > 0 {
			return func() tea.Msg { return RemoveAllMsg{} }
		}
	}
	m.clampScroll()
	return nil
}

// selected returns the breakpoint row under the cursor, ok=false on a file
// header or an empty list.
func (m *Model) selected() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.rows[m.cursor].header {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

// activate jumps to the breakpoint under row i; a file header jumps to its
// first breakpoint.
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
	msg := OpenLocationMsg{Path: r.path, Line: r.line}
	return func() tea.Msg { return msg }
}

// View renders the count header, the scrolled rows, and the key-hint footer.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	b.WriteString(m.renderRows(pal, m.bodyHeight()))
	b.WriteString(m.footer(pal))
	return b.String()
}

// headerLine totals the breakpoints.
func (m *Model) headerLine(pal *theme.Palette) string {
	n := 0
	if m.store != nil {
		n = m.store.Count()
	}
	counts := strconv.Itoa(n) + " breakpoint"
	if n != 1 {
		counts += "s"
	}
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" Breakpoints")
	return title + lipgloss.NewStyle().Faint(true).Render("   "+counts)
}

// renderRows draws the flattened list scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	if len(m.rows) == 0 {
		empty := lipgloss.NewStyle().Faint(true).Render(" (no breakpoints — ctrl+f8 toggles one on the cursor line)")
		return empty + strings.Repeat("\n", height)
	}
	m.clampScroll()
	base := lipgloss.NewStyle().Foreground(pal.Foreground)
	header := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.top + k
		if i < len(m.rows) {
			b.WriteString(m.renderRow(pal, base, header, i))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderRow draws one line: file headers accented, breakpoints glyph-tagged —
// ● enabled in the error tone (matching the gutter), ○ disabled faint.
func (m *Model) renderRow(pal *theme.Palette, base, header lipgloss.Style, i int) string {
	r := m.rows[i]
	var line string
	style := base
	if r.header {
		line = " " + m.shorten(r.path)
		style = header
	} else {
		glyph := "●"
		if !r.enabled {
			glyph = "○"
		}
		line = "   " + glyph + " " + strconv.Itoa(r.line+1)
		if r.preview != "" {
			line += "  " + r.preview
		}
		if r.enabled {
			style = style.Foreground(pal.Error)
		} else {
			style = style.Faint(true)
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

// footer shows the key hints.
func (m *Model) footer(pal *theme.Palette) string {
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter jump · space enable/disable · d delete · D delete all · j/k move"))
}

// shorten renders a store key through the injected shortener.
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

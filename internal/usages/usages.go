// Package usages is the Usages tool window (#1155): a singleton bottom-split
// pane holding the latest panel-targeted find-references results
// (lsp.referencesPanel), the persistent counterpart to the transient palette
// list lsp.references opens. It is a pure consumer — the LSP bridge delivers
// a UsagesMsg, the root model fills this pane; 'r' re-runs the request via
// the bridge-built Refresh continuation the message carried.
package usages

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/theme"
)

// row is one rendered line: a file header or one reference under it.
type row struct {
	header bool
	path   string
	ref    ilsp.Reference
}

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the Problems panel (#1024).
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	displayPath func(string) string

	// symbol names the searched identifier (title); loaded marks that a
	// result ever arrived, so the empty state can explain how to fill the
	// pane before the first search.
	symbol string
	loaded bool
	// refresh is the bridge-built continuation re-running the request at the
	// origin position it was created for ('r'). Best-effort after edits: the
	// stored position re-resolves as-is.
	refresh tea.Cmd

	rows   []row
	count  int // references behind the rows (title)
	files  int // distinct files holding them (title)
	cursor int
	top    int

	// Double-click detection mirrors the Problems panel (#514): activating a
	// row needs a second click on the same row within doubleClickWindow; now
	// is injectable so tests control the clock.
	lastClickRow int
	lastClickAt  time.Time
	now          func() time.Time
}

// New returns an empty pane; results arrive via Set.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, lastClickRow: -1, now: time.Now}
}

// SetDisplayPath injects the project-relative path shortener the app already
// uses for the finder; unset falls back to the raw (absolute) path.
func (m *Model) SetDisplayPath(f func(string) string) { m.displayPath = f }

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the pane focused (header + selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// Symbol reports the searched identifier (tests, title).
func (m *Model) Symbol() string { return m.symbol }

// Count reports the total number of references listed.
func (m *Model) Count() int { return m.count }

// Files reports how many distinct files hold the references.
func (m *Model) Files() int { return m.files }

// Rows reports the flattened row count (tests).
func (m *Model) Rows() int { return len(m.rows) }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// Set replaces the pane content with one find-references result: the symbol
// name for the title, the references grouped by file in server order (file
// order = first appearance, within-file order untouched), and the refresh
// continuation 'r' re-runs.
func (m *Model) Set(symbol string, refs []ilsp.Reference, refresh tea.Cmd) {
	m.symbol = symbol
	m.refresh = refresh
	m.loaded = true
	m.rows = nil
	m.count = len(refs)
	byPath := map[string][]ilsp.Reference{}
	var order []string
	for _, ref := range refs {
		if _, ok := byPath[ref.Path]; !ok {
			order = append(order, ref.Path)
		}
		byPath[ref.Path] = append(byPath[ref.Path], ref)
	}
	m.files = len(order)
	for _, path := range order {
		m.rows = append(m.rows, row{header: true, path: path})
		for _, ref := range byPath[path] {
			m.rows = append(m.rows, row{path: path, ref: ref})
		}
	}
	m.cursor, m.top = 0, 0
	if len(m.rows) > 1 {
		m.cursor = 1 // start on the first reference, not its file header
	}
	m.clampScroll()
}

// Update handles one message while the pane exists; only key presses reach
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
	case "r":
		// Re-run the request for the stored origin (#1155); best-effort
		// after edits — the position re-resolves as-is.
		return m.refresh
	case "enter":
		return m.activate(m.cursor)
	}
	m.clampScroll()
	return nil
}

// activate opens the reference under row i through the same navigation path
// the palette list uses (DefinitionMsg); a file header opens the file at its
// first reference.
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
	msg := ilsp.DefinitionMsg{Path: r.ref.Path, Line: r.ref.Line, Col: r.ref.Col}
	return func() tea.Msg { return msg }
}

// View renders the title header, the scrolled rows, and the key-hint footer.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	b.WriteString(m.renderRows(pal, m.bodyHeight()))
	b.WriteString(m.footer(pal))
	return b.String()
}

// Title is the pane header text: the searched symbol plus the totals —
// "Usages: Foo — 12 in 4 files" (#1155).
func (m *Model) Title() string {
	t := "Usages"
	if m.symbol != "" {
		t += ": " + m.symbol
	}
	if m.loaded {
		unit := " files"
		if m.files == 1 {
			unit = " file"
		}
		t += " — " + strconv.Itoa(m.count) + " in " + strconv.Itoa(m.files) + unit
	}
	return t
}

// headerLine renders the title accented, totals faint.
func (m *Model) headerLine(pal *theme.Palette) string {
	t := "Usages"
	if m.symbol != "" {
		t += ": " + m.symbol
	}
	head := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" " + t)
	if m.loaded {
		unit := " files"
		if m.files == 1 {
			unit = " file"
		}
		head += lipgloss.NewStyle().Faint(true).Render("   " + strconv.Itoa(m.count) + " in " + strconv.Itoa(m.files) + unit)
	}
	return head
}

// renderRows draws the flattened list scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	if len(m.rows) == 0 {
		return lipgloss.NewStyle().Faint(true).Render(" "+m.emptyText()) + strings.Repeat("\n", height)
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

// emptyText explains the empty pane per state.
func (m *Model) emptyText() string {
	if m.loaded {
		return "(no usages found)"
	}
	return "(no search yet — run Find Usages (Panel) from an editor)"
}

// renderRow draws one line: file headers accented, references as
// line:col plus the trimmed source-line preview.
func (m *Model) renderRow(pal *theme.Palette, base, header lipgloss.Style, i int) string {
	r := m.rows[i]
	var line string
	style := base
	if r.header {
		line = " " + m.shorten(r.path)
		style = header
	} else {
		pos := strconv.Itoa(r.ref.Line+1) + ":" + strconv.Itoa(r.ref.Col+1)
		line = "   " + pos + "  " + r.ref.Preview
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

// footer shows the key hints.
func (m *Model) footer(pal *theme.Palette) string {
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter open · r refresh · j/k move"))
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

// clip bounds one rendered line to the pane width.
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

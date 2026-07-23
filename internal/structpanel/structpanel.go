// Package structpanel is the Structure tool window (#1025): a side pane
// showing the focused buffer's symbol tree from LSP textDocument/documentSymbol
// — JetBrains' Structure view scaled to the terminal. The panel is pure
// presentation: the root model runs the LSP request through the registry
// command and feeds the converted tree in; enter/double-click emit a
// NavigateMsg the root model turns into a standard cursor jump (nav history
// records it like a definition jump).
package structpanel

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/lsp"
	"ike/internal/theme"
)

// NavigateMsg asks the root model to move the editor cursor to a symbol's
// position (0-based editor coordinates), through the standard open funnel so
// nav history records the jump.
type NavigateMsg struct {
	Path string
	Line int
	Col  int
}

// Row is one flattened symbol: its display fields plus the tree depth for
// indentation and the span the enclosing-symbol follow uses.
type Row struct {
	Name    string
	Detail  string
	Kind    int
	Depth   int
	Line    int
	Col     int
	EndLine int
}

// doubleClickWindow matches the explorer's and VCS panel's double-click delay.
const doubleClickWindow = 400 * time.Millisecond

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the VCS panel.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	path       string // file the rows belong to; "" until the first delivery
	noProvider bool
	rows       []Row
	cursor     int
	top        int
	current    int // enclosing symbol of the editor cursor (-1 none)

	lastClickRow int
	lastClickAt  time.Time
	now          func() time.Time
}

// New returns an empty panel awaiting its first symbol delivery.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, current: -1, lastClickRow: -1, now: time.Now}
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the panel focused (selection highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// Path reports which file the shown tree belongs to ("" before the first
// delivery) — the root model's staleness test.
func (m *Model) Path() string { return m.path }

// Rows exposes the flattened rows (tests).
func (m *Model) Rows() []Row { return m.rows }

// Cursor reports the selected row index (tests).
func (m *Model) Cursor() int { return m.cursor }

// Current reports the cursor-follow row index, -1 when none (tests).
func (m *Model) Current() int { return m.current }

// SetSymbols replaces the tree with a fresh delivery for path. A delivery for
// the same file keeps the selection on the same symbol name where possible; a
// new file resets it to the top.
func (m *Model) SetSymbols(path string, syms []lsp.SymbolNode, noProvider bool) {
	keep := ""
	if path == m.path && m.cursor < len(m.rows) {
		keep = m.rows[m.cursor].Name
	}
	m.path = path
	m.noProvider = noProvider
	m.rows = Flatten(syms)
	m.cursor, m.top, m.current = 0, 0, -1
	if keep != "" {
		for i, r := range m.rows {
			if r.Name == keep {
				m.cursor = i
				break
			}
		}
	}
	m.scrollToCursor()
}

// Flatten walks the symbol tree depth-first into display rows.
func Flatten(syms []lsp.SymbolNode) []Row {
	var out []Row
	var walk func(nodes []lsp.SymbolNode, depth int)
	walk = func(nodes []lsp.SymbolNode, depth int) {
		for _, n := range nodes {
			out = append(out, Row{
				Name:    n.Name,
				Detail:  n.Detail,
				Kind:    n.Kind,
				Depth:   depth,
				Line:    n.Line,
				Col:     n.Col,
				EndLine: n.EndLine,
			})
			walk(n.Children, depth+1)
		}
	}
	walk(syms, 0)
	return out
}

// Follow highlights the symbol enclosing the editor's cursor line (0-based):
// the deepest row whose [Line, EndLine] span contains it, falling back to the
// nearest preceding row. The highlight scrolls into view while the panel is
// unfocused, so it never fights the user's own scrolling.
func (m *Model) Follow(line int) {
	prev, encl := -1, -1
	for i, r := range m.rows {
		if r.Line > line {
			break
		}
		prev = i
		if line <= r.EndLine {
			// Depth-first order: a later containing row is a child of (or a
			// later sibling inside) the previous one, so the last containing
			// row is the most specific.
			encl = i
		}
	}
	best := encl
	if best < 0 {
		best = prev
	}
	m.current = best
	if best >= 0 && !m.focused {
		m.cursor = best
		m.scrollToCursor()
	}
}

// Update handles one message while the panel exists; the pane layer only
// routes key presses of the focused pane here.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		if len(m.rows) > 0 {
			m.cursor = len(m.rows) - 1
		}
	case "pgup":
		m.cursor = clampInt(m.cursor-m.bodyHeight(), 0, maxInt(0, len(m.rows)-1))
	case "pgdown":
		m.cursor = clampInt(m.cursor+m.bodyHeight(), 0, maxInt(0, len(m.rows)-1))
	case "enter":
		return m.navigate(m.cursor)
	default:
		return nil
	}
	m.scrollToCursor()
	return nil
}

// navigate emits the jump to row i's symbol.
func (m *Model) navigate(i int) tea.Cmd {
	if i < 0 || i >= len(m.rows) || m.path == "" {
		return nil
	}
	msg := NavigateMsg{Path: m.path, Line: m.rows[i].Line, Col: m.rows[i].Col}
	return func() tea.Msg { return msg }
}

// Wheel scrolls the list by delta rows (positive = down), like the VCS panel.
func (m *Model) Wheel(delta int) {
	m.top = clampInt(m.top+delta, 0, maxInt(0, len(m.rows)-1))
	m.cursor = clampInt(m.cursor, m.top, maxInt(m.top, m.top+m.bodyHeight()-1))
}

// Click handles one left click at content-local (x, y): a row click selects,
// a second click on the same row within the double-click window navigates.
func (m *Model) Click(x, y int) tea.Cmd {
	i := m.top + (y - 1)
	if y < 1 || i < 0 || i >= len(m.rows) {
		return nil
	}
	nowAt := m.now()
	double := m.lastClickRow == i && nowAt.Sub(m.lastClickAt) <= doubleClickWindow
	m.lastClickRow, m.lastClickAt = i, nowAt
	m.cursor = i
	if double {
		return m.navigate(i)
	}
	return nil
}

// View renders the header line plus the symbol rows.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.header(pal))
	height := m.bodyHeight()
	if len(m.rows) == 0 {
		b.WriteString("\n " + lipgloss.NewStyle().Faint(true).Render(m.emptyNotice()))
		for k := 1; k < height; k++ {
			b.WriteString("\n")
		}
		return b.String()
	}
	m.scrollToCursor()
	for k := 0; k < height; k++ {
		b.WriteString("\n")
		i := m.top + k
		if i >= len(m.rows) {
			continue
		}
		b.WriteString(m.renderRow(pal, i))
	}
	return b.String()
}

// header names the file the tree belongs to.
func (m *Model) header(pal *theme.Palette) string {
	name := "(no file)"
	if m.path != "" {
		name = baseName(m.path)
	}
	s := lipgloss.NewStyle().Foreground(pal.Secondary)
	if m.focused {
		s = lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	}
	return " " + s.Render(name) + " " + lipgloss.NewStyle().Faint(true).Render("("+strconv.Itoa(len(m.rows))+")")
}

// emptyNotice explains an empty list.
func (m *Model) emptyNotice() string {
	switch {
	case m.path == "":
		return "open a file to see its structure"
	case m.noProvider:
		return "no language server provides symbols for this file"
	default:
		return "no symbols"
	}
}

// renderRow draws one symbol row: indent, kind glyph, name, faint detail.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	r := m.rows[i]
	glyph := KindGlyph(r.Kind)
	line := " " + strings.Repeat("  ", r.Depth) + glyph + " " + r.Name
	if r.Detail != "" {
		line += " " + r.Detail
	}
	line = clip(line, m.width)
	style := lipgloss.NewStyle().Foreground(pal.Foreground)
	switch {
	case i == m.cursor && m.focused:
		style = style.Background(pal.Selection).Bold(true)
	case i == m.cursor:
		// Unfocused panes keep a muted cursor row (#1034) so refocusing
		// lands visibly, matching the explorer and the other list panes.
		style = style.Background(pal.SelectionMuted)
	case i == m.current:
		// The enclosing symbol of the editor cursor (auto-follow).
		style = style.Foreground(pal.Accent)
	}
	return style.Render(line)
}

// scrollToCursor keeps the selected row inside the visible window.
func (m *Model) scrollToCursor() {
	height := m.bodyHeight()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+height {
		m.top = m.cursor - height + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

// bodyHeight is the room below the header line.
func (m *Model) bodyHeight() int {
	h := m.height - 1
	if h < 1 {
		h = 1
	}
	return h
}

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// KindGlyph maps an LSP symbol kind to a one-cell glyph for the row prefix.
func KindGlyph(kind int) string {
	switch kind {
	case 1: // file
		return "◆"
	case 2, 3, 4: // module, namespace, package
		return "▤"
	case 5: // class
		return "C"
	case 6: // method
		return "m"
	case 7: // property
		return "p"
	case 8: // field
		return "f"
	case 9: // constructor
		return "c"
	case 10: // enum
		return "E"
	case 11: // interface
		return "I"
	case 12: // function
		return "ƒ"
	case 13: // variable
		return "v"
	case 14: // constant
		return "K"
	case 15, 16: // string, number
		return "l"
	case 17: // boolean
		return "b"
	case 18: // array
		return "a"
	case 19: // object
		return "o"
	case 20: // key
		return "k"
	case 22: // enum member
		return "e"
	case 23: // struct
		return "S"
	case 24: // event
		return "!"
	case 25: // operator
		return "±"
	case 26: // type parameter
		return "T"
	default:
		return "•"
	}
}

// clip truncates a line to width terminal cells (rune-approximated), matching
// the other tool panels' clipping.
func clip(s string, width int) string {
	if width <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

// baseName is filepath.Base without the import, over slash or backslash.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

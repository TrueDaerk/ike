// Package palette implements the command palette overlay (Roadmap 0070): a single
// centered, floating input that fronts every action in IKE. A leading prefix rune
// selects a Mode — ":" runs registered commands, "@" finds files — and the chosen
// result is dispatched as a tea.Msg the root model applies. The palette owns no
// command store: it is a consumer of the plugin registry and a pure
// presentation-plus-routing layer. The core is prefix-agnostic, so new modes are
// added by registering one more Mode.
package palette

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Config tunes a Palette. Zero values select sensible defaults, so the empty
// Config is valid.
type Config struct {
	MaxResults    int    // result rows shown; <=0 selects defaultMaxResults
	DefaultPrefix rune   // mode used when the query has no recognised prefix; 0 selects the first mode
	Accent        string // border/highlight colour; "" selects defaultAccent
}

const (
	defaultMaxResults = 12
	defaultAccent     = "69"
	dimColor          = "240"
	selectedBg        = "236"
)

// Palette is the overlay model. It holds the open/closed state, the raw query
// (including its prefix rune), the ranked results for that query, the selection,
// and the captured Context. It is value-routed by the root model: keys are
// forwarded while open, and View is composited on top of the layout.
type Palette struct {
	modes    []Mode
	byPrefix map[rune]Mode
	def      Mode

	open     bool
	query    string
	items    []Item
	selected int
	top      int // first visible row (scroll window into items)
	cx       Context

	width, height int
	maxResults    int
	accent        string
}

// New builds a palette from cfg and the ordered modes (first is the default when
// no prefix matches, unless cfg.DefaultPrefix overrides it).
func New(cfg Config, modes ...Mode) *Palette {
	p := &Palette{
		modes:      modes,
		byPrefix:   make(map[rune]Mode, len(modes)),
		maxResults: cfg.MaxResults,
		accent:     cfg.Accent,
	}
	if p.maxResults <= 0 {
		p.maxResults = defaultMaxResults
	}
	if p.accent == "" {
		p.accent = defaultAccent
	}
	for _, m := range modes {
		p.byPrefix[m.Prefix()] = m
	}
	if len(modes) > 0 {
		p.def = modes[0]
	}
	if cfg.DefaultPrefix != 0 {
		if m, ok := p.byPrefix[cfg.DefaultPrefix]; ok {
			p.def = m
		}
	}
	return p
}

// IsOpen reports whether the palette is currently shown.
func (p *Palette) IsOpen() bool { return p.open }

// Open shows the palette for context cx with an empty query, recomputing the
// initial result list (the default mode's full listing).
func (p *Palette) Open(cx Context) {
	p.open = true
	p.query = ""
	p.selected = 0
	p.top = 0
	p.cx = cx
	p.recompute()
}

// Close hides the palette without side effects.
func (p *Palette) Close() { p.open = false }

// SetSize records the terminal size used to size the centered box.
func (p *Palette) SetSize(width, height int) { p.width, p.height = width, height }

// Update handles a key while the palette is open and returns a command for the
// activated item, if any. esc closes; enter activates the selection and closes;
// up/down/ctrl+p/ctrl+n navigate; backspace/ctrl+u edit; typed runes extend the
// query. The root model calls this only while IsOpen, and the palette consumes
// every key (the overlay is modal).
func (p *Palette) Update(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		p.Close()
		return nil
	case tea.KeyEnter:
		return p.activate()
	case tea.KeyUp, tea.KeyCtrlP:
		p.move(-1)
		return nil
	case tea.KeyDown, tea.KeyCtrlN:
		p.move(1)
		return nil
	case tea.KeyBackspace:
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.recompute()
		}
		return nil
	case tea.KeyCtrlU:
		p.query = ""
		p.recompute()
		return nil
	case tea.KeySpace:
		p.query += " "
		p.recompute()
		return nil
	case tea.KeyRunes:
		p.query += string(msg.Runes)
		p.recompute()
		return nil
	}
	return nil
}

// activate emits the selected item's message and closes the palette. With no
// results it is a dismiss-less no-op (the palette stays open).
func (p *Palette) activate() tea.Cmd {
	if p.selected < 0 || p.selected >= len(p.items) {
		return nil
	}
	msg := p.items[p.selected].Msg
	p.Close()
	if msg == nil {
		return nil
	}
	return func() tea.Msg { return msg }
}

// move changes the selection by delta, clamped, and scrolls the visible window.
func (p *Palette) move(delta int) {
	if len(p.items) == 0 {
		return
	}
	p.selected += delta
	if p.selected < 0 {
		p.selected = 0
	}
	if p.selected >= len(p.items) {
		p.selected = len(p.items) - 1
	}
	p.scrollToSelected()
}

// mode resolves the active mode and the query body (prefix stripped). A leading
// rune that names a registered mode selects it; otherwise the default mode ranks
// the whole query.
func (p *Palette) mode() (Mode, string) {
	if p.query != "" {
		r := []rune(p.query)
		if m, ok := p.byPrefix[r[0]]; ok {
			return m, string(r[1:])
		}
	}
	return p.def, p.query
}

// recompute re-ranks results for the current query and resets the selection.
func (p *Palette) recompute() {
	m, body := p.mode()
	if m == nil {
		p.items = nil
	} else {
		p.items = m.Results(body, p.cx)
	}
	p.selected = 0
	p.top = 0
}

// scrollToSelected keeps the selected row within the visible window.
func (p *Palette) scrollToSelected() {
	if p.selected < p.top {
		p.top = p.selected
	}
	if p.selected >= p.top+p.maxResults {
		p.top = p.selected - p.maxResults + 1
	}
}

// View renders the centered palette box, or empty when closed or unsized. The
// caller composites it over the layout (e.g. overlay.Center).
func (p *Palette) View() string {
	if !p.open || p.width <= 0 {
		return ""
	}
	boxW := p.boxWidth()
	inner := boxW - 4 // border (2) + horizontal padding (2)
	if inner < 1 {
		inner = 1
	}

	accent := lipgloss.Color(p.accent)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor))

	prompt := dim.Render(p.promptGlyph()) + " " + p.queryView(inner-2)
	sep := dim.Render(strings.Repeat("─", inner))
	body := lipgloss.JoinVertical(lipgloss.Left, prompt, sep, p.list(inner))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 1).
		Width(inner)
	return box.Render(body)
}

// promptGlyph is the leading glyph echoing the active mode: the mode's prefix
// rune when one is typed, else a generic ">".
func (p *Palette) promptGlyph() string {
	if p.query != "" {
		r := []rune(p.query)
		if _, ok := p.byPrefix[r[0]]; ok {
			return string(r[0])
		}
	}
	return ">"
}

// queryView renders the query body (prefix stripped) with a cursor bar, or the
// active mode's placeholder when empty.
func (p *Palette) queryView(width int) string {
	_, body := p.mode()
	if body == "" {
		ph := ""
		if m, _ := p.mode(); m != nil {
			ph = m.Placeholder()
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor)).Render(ph)
	}
	return ansi.Truncate(body+"▏", width, "…")
}

// list renders the visible result rows with selection and match highlighting, or
// a dim "no results" line when the query matched nothing.
func (p *Palette) list(width int) string {
	if len(p.items) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor)).Render("no results")
	}
	end := p.top + p.maxResults
	if end > len(p.items) {
		end = len(p.items)
	}
	var rows []string
	for i := p.top; i < end; i++ {
		rows = append(rows, p.row(p.items[i], i == p.selected, width))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// row renders a single result line: a selection marker, the highlighted title,
// and a right-aligned dim detail, all clamped to width.
func (p *Palette) row(it Item, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Render("❯ ")
	}
	avail := width - 2 // marker columns
	title := highlight(it.Title, it.Spans, p.accent, selected)
	line := marker + title
	if it.Detail != "" {
		detail := lipgloss.NewStyle().Foreground(lipgloss.Color(dimColor)).Render(it.Detail)
		gap := avail - ansi.StringWidth(it.Title) - ansi.StringWidth(it.Detail)
		if gap >= 1 {
			line = marker + title + strings.Repeat(" ", gap) + detail
		}
	}
	style := lipgloss.NewStyle().MaxWidth(width)
	if selected {
		style = style.Background(lipgloss.Color(selectedBg)).Width(width)
	}
	return style.Render(line)
}

// highlight renders title with the matched rune spans emphasised in the accent
// colour. spans index runes of title; out-of-range indices are ignored.
func highlight(title string, spans []int, accent string, selected bool) string {
	if len(spans) == 0 {
		return title
	}
	hit := make(map[int]bool, len(spans))
	for _, s := range spans {
		hit[s] = true
	}
	matchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Bold(true)
	var b strings.Builder
	for i, r := range []rune(title) {
		if hit[i] {
			b.WriteString(matchStyle.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// boxWidth is the outer width of the palette box: 60% of the terminal, clamped to
// a readable floor and the terminal minus a margin.
func (p *Palette) boxWidth() int {
	w := p.width * 6 / 10
	if w < 40 {
		w = 40
	}
	if w > p.width-4 {
		w = p.width - 4
	}
	if w < 1 {
		w = 1
	}
	return w
}

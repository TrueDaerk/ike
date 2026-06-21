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
	keyCapBg          = "238" // background of the right-aligned key-binding chip
	keyCapFg          = "252" // foreground of the key-binding chip
	minBoxWidth       = 36    // floor for the centered box
	minAnchorWidth    = 20    // floor for an editor-anchored box
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

	// locked, when non-nil, pins the palette to a single mode (no prefix
	// switching): a slimmed file-only palette opened from the editor uses this.
	locked Mode
	// anchored renders the box at (anchorX, anchorY) sized to anchorW instead of
	// centered, so a palette opened from a pane floats over that pane.
	anchored         bool
	anchorX, anchorY int
	anchorW          int

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

// Open shows the centered palette for context cx with an empty query, recomputing
// the initial result list (the default mode's full listing). It clears any lock
// or anchor from a prior open.
func (p *Palette) Open(cx Context) {
	p.reset(cx)
	p.locked = nil
	p.anchored = false
	p.recompute()
}

// OpenAnchored shows a slimmed palette pinned to the mode with the given prefix
// (no mode switching), floated at (x, y) sized to w. It is how the editor opens a
// file-only finder over its own pane. An unknown prefix falls back to the default
// centered palette.
func (p *Palette) OpenAnchored(cx Context, prefix rune, x, y, w int) {
	m, ok := p.byPrefix[prefix]
	if !ok {
		p.Open(cx)
		return
	}
	p.reset(cx)
	p.locked = m
	p.anchored = true
	p.anchorX, p.anchorY, p.anchorW = x, y, w
	p.recompute()
}

// reset clears the per-open transient state.
func (p *Palette) reset(cx Context) {
	p.open = true
	p.query = ""
	p.selected = 0
	p.top = 0
	p.cx = cx
}

// Anchored reports whether the box should be placed at its anchor rather than
// centered. AnchorPos returns that placement.
func (p *Palette) Anchored() bool        { return p.anchored }
func (p *Palette) AnchorPos() (int, int) { return p.anchorX, p.anchorY }

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
	if p.locked != nil {
		return p.locked, p.query // pinned mode: the whole query is the body
	}
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
	if p.locked != nil {
		return string(p.locked.Prefix())
	}
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

// row renders a single result line: a selection marker, the highlighted title on
// the left, and the key binding (Detail) as a highlighted chip pinned to the
// right. The title is truncated first so the binding chip is never dropped.
func (p *Palette) row(it Item, selected bool, width int) string {
	const markerW = 2
	marker := "  "
	if selected {
		marker = lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Render("❯ ")
	}

	detail, detailW := "", 0
	if it.Detail != "" {
		detail = lipgloss.NewStyle().
			Background(lipgloss.Color(keyCapBg)).
			Foreground(lipgloss.Color(keyCapFg)).
			Bold(true).
			Render(" " + it.Detail + " ")
		detailW = ansi.StringWidth(it.Detail) + 2
	}

	avail := width - markerW
	titleMax := avail - detailW - 1 // keep at least one space before the chip
	if titleMax < 1 {
		titleMax = 1
	}
	title, titleW := highlight(it.Title, it.Spans, p.accent, titleMax)

	gap := avail - titleW - detailW
	if gap < 1 {
		gap = 1
	}
	line := marker + title + strings.Repeat(" ", gap) + detail

	style := lipgloss.NewStyle().MaxWidth(width)
	if selected {
		style = style.Background(lipgloss.Color(selectedBg)).Width(width)
	}
	return style.Render(line)
}

// highlight renders title with the matched rune spans emphasised in the accent
// colour, truncated to at most maxW display cells (with an ellipsis). It returns
// the styled string and its display width. Spans index runes of the full title;
// indices past the truncation point are dropped.
func highlight(title string, spans []int, accent string, maxW int) (string, int) {
	runes := []rune(title)
	truncated := false
	if maxW >= 1 && len(runes) > maxW {
		runes = runes[:maxW-1]
		truncated = true
	}
	hit := make(map[int]bool, len(spans))
	for _, s := range spans {
		hit[s] = true
	}
	matchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Bold(true)
	var b strings.Builder
	for i, r := range runes {
		if hit[i] {
			b.WriteString(matchStyle.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	w := len(runes)
	if truncated {
		b.WriteString("…")
		w++
	}
	return b.String(), w
}

// boxWidth is the outer width of the palette box. Anchored, it tracks the host
// pane's width; centered, it is half the terminal, both clamped to a readable
// floor and the space actually available.
func (p *Palette) boxWidth() int {
	var w, floor, room int
	if p.anchored {
		w, floor, room = p.anchorW, minAnchorWidth, p.width-p.anchorX
	} else {
		w, floor, room = p.width/2, minBoxWidth, p.width-4
	}
	if w < floor {
		w = floor
	}
	if w > room {
		w = room
	}
	if w < 1 {
		w = 1
	}
	return w
}

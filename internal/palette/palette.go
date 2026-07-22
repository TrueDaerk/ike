// Package palette implements the command palette overlay (Roadmap 0070): a single
// centered, floating input that fronts every action in IKE. A leading prefix rune
// selects a Mode — ":" runs registered commands, "@" finds files — and the chosen
// result is dispatched as a tea.Msg the root model applies. The palette owns no
// command store: it is a consumer of the plugin registry and a pure
// presentation-plus-routing layer. The core is prefix-agnostic, so new modes are
// added by registering one more Mode.
package palette

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
	"ike/internal/ui"
)

// Config tunes a Palette. Zero values select sensible defaults, so the empty
// Config is valid.
type Config struct {
	MaxResults    int    // result rows shown; <=0 selects defaultMaxResults
	DefaultPrefix rune   // mode used when the query has no recognised prefix; 0 selects the first mode
	Accent        string // border/highlight colour override; "" follows the theme
}

const (
	defaultMaxResults = 12
	minBoxWidth       = 36 // floor for the centered box
	minAnchorWidth    = 20 // floor for an editor-anchored box
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
	cur      int // rune cursor within query, including the prefix rune (#763)
	items    []Item
	selected int
	top      int // first visible row (scroll window into items)
	cx       Context

	// liveGen pins debounce ticks to the newest query edit (#295).
	liveGen int

	// locked, when non-nil, pins the palette to a single mode (no prefix
	// switching): a slimmed file-only palette opened from the editor uses this.
	locked Mode
	// anchored renders the box at (anchorX, anchorY) sized to anchorW instead of
	// centered, so a palette opened from a pane floats over that pane.
	anchored         bool
	anchorX, anchorY int
	anchorW          int

	width, height int
	sizes         *ui.WinSizes // optional persisted resize deltas (#774)

	// The optional left column of a SideMode (#778): its items for the
	// current query, its selection, and whether it holds the column focus.
	sideItems []Item
	sideSel   int
	sideFocus bool
	// sideManual marks an explicit column switch (tab/arrows/click) for the
	// current query (#819): while set, recompute keeps the user's column
	// instead of auto-placing the focus; any query edit clears it.
	sideManual bool
	maxResults    int
	accent        string         // config override; "" follows the theme
	pal           *theme.Palette // active theme (Roadmap 0110); nil = default
}

// SetPalette threads the active theme palette in (Roadmap 0110); chrome colors
// (accent, dim, selection, key chips) derive from its ui slots.
func (p *Palette) SetPalette(pal *theme.Palette) { p.pal = pal }

// theme returns the active palette, defaulting when none was threaded in.
func (p *Palette) theme() *theme.Palette {
	if p.pal != nil {
		return p.pal
	}
	return theme.DefaultPalette()
}

// accentColor is the border/highlight colour: the config override when set,
// else the theme's focused-border slot.
func (p *Palette) accentColor() color.Color {
	if p.accent != "" {
		return lipgloss.Color(p.accent)
	}
	return p.theme().BorderFocus
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

// OpenLocked shows the centered palette pinned to the mode with the given
// prefix (no mode switching) — how project.goToFile opens the file finder from
// anywhere. An unknown prefix falls back to the default centered palette.
func (p *Palette) OpenLocked(cx Context, prefix rune) {
	m, ok := p.byPrefix[prefix]
	if !ok {
		p.Open(cx)
		return
	}
	p.reset(cx)
	p.locked = m
	p.anchored = false
	p.recompute()
}

// reset clears the per-open transient state.
func (p *Palette) reset(cx Context) {
	p.open = true
	p.query = ""
	p.cur = 0
	p.selected = 0
	p.top = 0
	p.cx = cx
	p.sideFocus = false
	p.sideSel = 0
	p.sideItems = nil
	p.sideManual = false
}

// side returns the locked mode's SideMode extension, nil when the current
// open has no left column (#778).
func (p *Palette) side() SideMode {
	if s, ok := p.locked.(SideMode); ok && !p.anchored {
		return s
	}
	return nil
}

// Anchored reports whether the box should be placed at its anchor rather than
// centered. AnchorPos returns that placement.
func (p *Palette) Anchored() bool        { return p.anchored }
func (p *Palette) AnchorPos() (int, int) { return p.anchorX, p.anchorY }

// Close hides the palette without side effects.
func (p *Palette) Close() { p.open = false }

// SetSize records the terminal size used to size the centered box.
func (p *Palette) SetSize(width, height int) { p.width, p.height = width, height }

// SetSizeStore installs the persisted resize deltas (#774): ctrl+shift+arrows
// widen/narrow the centered box and grow/shrink the visible result rows.
func (p *Palette) SetSizeStore(s *ui.WinSizes) { p.sizes = s }

// winKind is the persistence key for the palette window.
const winKind = "palette"

// AdjustSize applies one mouse-drag resize step (#933) without persisting;
// the host flushes the store when the drag ends. Width in columns, height in
// result rows (one row per cell dragged).
func (p *Palette) AdjustSize(ddw, ddh int) {
	if p.sizes == nil {
		return
	}
	p.sizes.Nudge(winKind, ddw, ddh)
	p.scrollToSelected()
}

// visibleRows is the effective result-window height: the configured
// maxResults plus the user's stored resize delta, floored at 3 (#774).
func (p *Palette) visibleRows() int {
	_, dh := p.sizes.Get(winKind)
	return ui.ClampDelta(p.maxResults, dh, 3, 99)
}

// Update handles a key while the palette is open and returns a command for the
// activated item, if any. esc closes; enter activates the selection and closes;
// up/down/ctrl+p/ctrl+n navigate; backspace/ctrl+u edit; typed runes extend the
// query. The root model calls this only while IsOpen, and the palette consumes
// every key (the overlay is modal).
func (p *Palette) Update(msg tea.KeyPressMsg) tea.Cmd {
	// Resize chords (#774) first — the plain-arrow cases below match on Code
	// alone and would swallow the modified presses.
	if ddw, ddh, ok := ui.ResizeDelta(msg.String()); ok && p.sizes != nil {
		p.sizes.Adjust(winKind, ddw, ddh)
		p.scrollToSelected()
		return nil
	}
	// Column focus for a SideMode open (#778): tab toggles between the left
	// (projects) and right (files) columns; on an empty query the plain
	// arrows switch too (with text present they stay cursor keys).
	if len(p.sideItems) > 0 {
		switch {
		case msg.Code == tea.KeyTab && msg.Mod == 0:
			p.sideFocus = !p.sideFocus
			p.sideManual = true
			return nil
		case msg.Code == tea.KeyLeft && msg.Mod == 0 && p.query == "":
			p.sideFocus = true
			p.sideManual = true
			return nil
		case msg.Code == tea.KeyRight && msg.Mod == 0 && p.query == "":
			p.sideFocus = false
			p.sideManual = true
			return nil
		}
		if p.sideFocus {
			switch {
			case msg.Code == tea.KeyUp, msg.Code == 'p' && msg.Mod == tea.ModCtrl:
				if p.sideSel > 0 {
					p.sideSel--
				}
				return nil
			case msg.Code == tea.KeyDown, msg.Code == 'n' && msg.Mod == tea.ModCtrl:
				if p.sideSel < len(p.sideItems)-1 {
					p.sideSel++
				}
				return nil
			case msg.Code == tea.KeyEnter:
				return p.activateSide()
			}
		}
	}
	// The aux action (#820): shift+delete emits the focused row's Aux msg —
	// e.g. close a background workspace — and keeps the palette open; the
	// caller refreshes the lists via Refresh.
	if msg.Code == tea.KeyDelete && msg.Mod == tea.ModShift {
		return p.auxCmd()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		p.Close()
		return nil
	case msg.Code == tea.KeyEnter:
		return p.activate()
	case msg.Code == tea.KeyUp, msg.Code == 'p' && msg.Mod == tea.ModCtrl:
		p.move(-1)
		return nil
	case msg.Code == tea.KeyDown, msg.Code == 'n' && msg.Mod == tea.ModCtrl:
		p.move(1)
		return nil
	case msg.Code == tea.KeyTab:
		// Ask the active mode to extend the query (path completion, #542).
		m, body := p.mode()
		if c, ok := m.(Completer); ok {
			if out := c.Complete(body); out != body {
				p.query = p.query[:len(p.query)-len(body)] + out
				p.cur = len([]rune(p.query))
				p.sideManual = false
				p.recompute()
				return p.liveKick()
			}
		}
		return nil
	case msg.Code == 'u' && msg.Mod == tea.ModCtrl:
		p.query = ""
		p.cur = 0
		p.sideManual = false
		p.recompute()
		return p.liveKick()
	}
	// Everything else is single-line editing on the query (#763): cursor
	// motions, word ops, backspace/delete, printable insertion.
	if out, ncur, handled, changed := ui.EditKey(msg, p.query, p.cur); handled {
		p.query, p.cur = out, ncur
		if changed {
			p.sideManual = false
			p.recompute()
			return p.liveKick()
		}
	}
	return nil
}

// auxCmd emits the focused row's Aux msg (#820), keeping the palette open;
// rows without an Aux action are inert.
func (p *Palette) auxCmd() tea.Cmd {
	it, ok := p.focusedItem()
	if !ok || it.Aux == nil {
		return nil
	}
	msg := it.Aux
	return func() tea.Msg { return msg }
}

// focusedItem resolves the row the keyboard focus is on: the side column's
// selection while it holds the focus, else the main list's.
func (p *Palette) focusedItem() (Item, bool) {
	if p.sideFocus {
		if p.sideSel >= 0 && p.sideSel < len(p.sideItems) {
			return p.sideItems[p.sideSel], true
		}
		return Item{}, false
	}
	if p.selected >= 0 && p.selected < len(p.items) {
		return p.items[p.selected], true
	}
	return Item{}, false
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

// activateSide emits the selected left-column item's message and closes the
// palette (#778). With no side selection it is a no-op.
func (p *Palette) activateSide() tea.Cmd {
	if p.sideSel < 0 || p.sideSel >= len(p.sideItems) {
		return nil
	}
	msg := p.sideItems[p.sideSel].Msg
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

// Click maps a left press at box-relative (x, y) onto the row layout (#820):
// a press on a result row activates it; a press on the row's right-pinned
// "✕" zone emits its Aux action instead, keeping the palette open. Presses
// on chrome (prompt, separator, heading, divider) are inert.
func (p *Palette) Click(x, y int) tea.Cmd {
	x -= 2 // border + horizontal padding
	y -= 1 // top border
	inner := p.boxWidth() - 4
	if inner < 1 || y < 2 || x < 0 || x >= inner {
		return nil // prompt row, separator, or outside the content
	}
	mainX, mainW := 0, inner
	if len(p.sideItems) > 0 {
		sideW := sideWidth(inner)
		mainX, mainW = sideW+3, inner-sideW-3
		if mainW < 10 {
			mainW = 10
		}
		if x < sideW { // side column: heading at y==2, items below
			idx := y - 3
			if idx < 0 || idx >= len(p.sideItems) {
				return nil
			}
			p.sideFocus, p.sideSel, p.sideManual = true, idx, true
			it := p.sideItems[idx]
			if it.Aux != nil && x >= sideW-auxGlyphW {
				return p.auxCmd()
			}
			return p.activateSide()
		}
		if x < mainX {
			return nil // the divider strip
		}
	}
	idx := p.top + (y - 2)
	if idx < 0 || idx >= len(p.items) {
		return nil
	}
	p.sideFocus = false
	p.sideManual = true
	p.selected = idx
	it := p.items[idx]
	if it.Aux != nil && x >= mainX+mainW-auxGlyphW {
		return p.auxCmd()
	}
	return p.activate()
}

// sideWidth is the left column's width for an inner content width (#778),
// shared by the renderer and the click mapping.
func sideWidth(inner int) int {
	w := inner / 3
	if w < 18 {
		w = 18
	}
	if w > 34 {
		w = 34
	}
	return w
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
	if s := p.side(); s != nil {
		p.sideItems = s.SideResults(body, p.cx)
	} else {
		p.sideItems = nil
	}
	p.sideSel = 0
	if len(p.sideItems) == 0 {
		p.sideFocus = false
	} else if !p.sideManual {
		p.sideFocus = p.autoSideFocus(body)
	}
}

// autoSideFocus decides which column holds the focus after a recompute
// (#819): the side (projects) column when the main list is empty, or when
// its top result strictly outscores the main list's — a project matching the
// query better than any file makes enter open the project. Files win ties,
// and an empty query always starts on a non-empty files list.
func (p *Palette) autoSideFocus(query string) bool {
	if len(p.items) == 0 {
		return true
	}
	if query == "" {
		return false
	}
	return p.sideItems[0].Score > p.items[0].Score
}

// scrollToSelected keeps the selected row within the visible window.
func (p *Palette) scrollToSelected() {
	if p.selected < p.top {
		p.top = p.selected
	}
	if p.selected >= p.top+p.visibleRows() {
		p.top = p.selected - p.visibleRows() + 1
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

	accent := p.accentColor()
	dim := lipgloss.NewStyle().Foreground(p.theme().Border)

	prompt := dim.Render(p.promptGlyph()) + " " + p.queryView(inner-2)
	sep := dim.Render(strings.Repeat("─", inner))
	rows := p.list(inner)
	// A SideMode open (#778) renders the left column (e.g. Recent Projects)
	// beside the main list, separated by a dim rule.
	if len(p.sideItems) > 0 {
		sideW := sideWidth(inner)
		mainW := inner - sideW - 3
		if mainW < 10 {
			mainW = 10
		}
		side := p.sideView(sideW)
		main := p.list(mainW)
		h := lipgloss.Height(side)
		if mh := lipgloss.Height(main); mh > h {
			h = mh
		}
		div := strings.TrimRight(strings.Repeat(dim.Render("│")+"\n", h), "\n")
		rows = lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(sideW).Render(side), " ", div, " ", main)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, prompt, sep, rows)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 1).
		Width(boxW) // Width is the outer, bordered width here, not the content width
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
		return lipgloss.NewStyle().Foreground(p.theme().Border).Render(ph)
	}
	pl := len([]rune(p.query)) - len([]rune(body))
	return ansi.Truncate(ui.CursorView(body, p.cur-pl), width, "…")
}

// list renders the visible result rows with selection and match highlighting, or
// a dim "no results" line when the query matched nothing.
func (p *Palette) list(width int) string {
	if len(p.items) == 0 {
		return lipgloss.NewStyle().Foreground(p.theme().Border).Render("no results")
	}
	end := p.top + p.visibleRows()
	if end > len(p.items) {
		end = len(p.items)
	}
	var rows []string
	for i := p.top; i < end; i++ {
		rows = append(rows, p.row(p.items[i], i == p.selected, width))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// sideView renders the left column (#778): the SideMode's dim heading — the
// accent color marks which column holds the focus — and its items, capped at
// the visible-row window.
func (p *Palette) sideView(width int) string {
	s := p.side()
	if s == nil {
		return ""
	}
	head := lipgloss.NewStyle().Foreground(p.theme().Border)
	if p.sideFocus {
		head = lipgloss.NewStyle().Foreground(p.accentColor()).Bold(true)
	}
	lines := []string{head.Render(s.SideTitle())}
	end := len(p.sideItems)
	if max := p.visibleRows() - 1; end > max {
		end = max
	}
	for i := 0; i < end; i++ {
		lines = append(lines, p.sideRow(p.sideItems[i], p.sideFocus && i == p.sideSel, width))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// auxGlyphW is the right-pinned "✕" affordance's width (glyph + one space).
const auxGlyphW = 2

// sideRow renders one left-column line: marker + highlighted title, an
// optional Badge (#820, dim accent), and the right-pinned "✕" for rows with
// an Aux action, truncated to the column width.
func (p *Palette) sideRow(it Item, selected bool, width int) string {
	const markerW = 2
	marker := "  "
	if selected {
		marker = lipgloss.NewStyle().Foreground(p.accentColor()).Render("❯ ")
	}
	badge, badgeW := p.badgeView(it)
	auxW := 0
	if it.Aux != nil {
		auxW = auxGlyphW
	}
	title, titleW := highlight(it.Title, it.Spans, p.accentColor(), width-markerW-badgeW-auxW)
	line := marker + title + badge
	if it.Aux != nil {
		gap := width - markerW - titleW - badgeW - auxGlyphW
		if gap < 1 {
			gap = 1
		}
		line += strings.Repeat(" ", gap) + lipgloss.NewStyle().Foreground(p.theme().Border).Render("✕ ")
	}
	style := lipgloss.NewStyle().MaxWidth(width)
	if selected {
		style = style.Background(p.theme().Panel).Width(width)
	}
	return style.Render(line)
}

// badgeView renders an item's Badge (#820) as a dim-accent suffix.
func (p *Palette) badgeView(it Item) (string, int) {
	if it.Badge == "" {
		return "", 0
	}
	return " " + lipgloss.NewStyle().Foreground(p.accentColor()).Render(it.Badge),
		1 + ansi.StringWidth(it.Badge)
}

// row renders a single result line: a selection marker, the highlighted title on
// the left, and the key binding (Detail) as a highlighted chip pinned to the
// right. The title is truncated first so the binding chip is never dropped.
func (p *Palette) row(it Item, selected bool, width int) string {
	const markerW = 2
	marker := "  "
	if selected {
		marker = lipgloss.NewStyle().Foreground(p.accentColor()).Render("❯ ")
	}

	detail, detailW := "", 0
	if it.Detail != "" {
		detail = lipgloss.NewStyle().
			Background(p.theme().SelectionMuted).
			Foreground(p.theme().Foreground).
			Bold(true).
			Render(" " + it.Detail + " ")
		detailW = ansi.StringWidth(it.Detail) + 2
	}

	badge, badgeW := p.badgeView(it)
	auxW := 0
	aux := ""
	if it.Aux != nil {
		auxW = auxGlyphW
		aux = lipgloss.NewStyle().Foreground(p.theme().Border).Render(" ✕")
	}
	avail := width - markerW
	titleMax := avail - detailW - badgeW - auxW - 1 // keep a space before the chip
	if titleMax < 1 {
		titleMax = 1
	}
	title, titleW := highlight(it.Title, it.Spans, p.accentColor(), titleMax)

	gap := avail - titleW - badgeW - detailW - auxW
	if gap < 1 {
		gap = 1
	}
	line := marker + title + badge + strings.Repeat(" ", gap) + detail + aux

	style := lipgloss.NewStyle().MaxWidth(width)
	if selected {
		style = style.Background(p.theme().Panel).Width(width)
	}
	return style.Render(line)
}

// highlight renders title with the matched rune spans emphasised in the accent
// colour, truncated to at most maxW display cells (with an ellipsis). It returns
// the styled string and its display width. Spans index runes of the full title;
// indices past the truncation point are dropped.
func highlight(title string, spans []int, accent color.Color, maxW int) (string, int) {
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
	matchStyle := lipgloss.NewStyle().Foreground(accent).Bold(true)
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
		// User resize (#774): the stored width delta widens/narrows the
		// centered box; the floor/room clamps below re-bound it live.
		dw, _ := p.sizes.Get(winKind)
		w += dw
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

package settings

// marketplace_page.go is the plugin marketplace page (Roadmap 0310, #446):
// browse the catalog (internal/market), review a plugin's requested
// capabilities, and install/update/remove with the engine. Install is only
// reachable from the expanded detail view — the capability list is on screen
// before the action exists, which is the trust model's review step. All
// network and disk work happens inside returned tea.Cmds (#123); results come
// back as MarketCatalogMsg/MarketActionMsg through the panel's Deliver
// routing. Newly installed plugins load on the next start (the runtime scans
// at startup), so every successful install/update shows the restart notice.

import (
	"context"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/market"
	"ike/internal/theme"
)

// MarketEngine is the slice of market.Engine the page uses; tests fake it.
type MarketEngine interface {
	Installed() (map[string]market.Installed, error)
	Install(ctx context.Context, entry market.Entry) error
	Remove(name string) error
}

// MarketFetcher fetches the catalog at url; production is market.Client.
type MarketFetcher func(ctx context.Context, url string) (market.Index, []string, error)

// MarketCatalogMsg is one finished catalog fetch.
type MarketCatalogMsg struct {
	Index market.Index
	Diags []string
	Err   error
}

// MarketActionMsg is one finished install/update/remove.
type MarketActionMsg struct {
	Name   string
	Action string // "install", "update", "remove"
	Err    error
}

// MarketplacePage implements PageModel (and MsgReceiver).
type MarketplacePage struct {
	engine MarketEngine
	fetch  MarketFetcher
	pal    *theme.Palette

	catalog   *market.Index
	diags     []string
	fetchErr  string
	fetching  bool
	installed map[string]market.Installed

	sel      int
	off      int // list scroll offset (#885)
	listH    int // list-window height of the last render (mouse hit-testing)
	expanded map[string]bool
	busy     map[string]bool
	rowNote  map[string]string
	restart  bool
}

// NewMarketplacePage builds the page from its injected seams.
func NewMarketplacePage(engine MarketEngine, fetch MarketFetcher) *MarketplacePage {
	return &MarketplacePage{
		engine:    engine,
		fetch:     fetch,
		installed: map[string]market.Installed{},
		expanded:  map[string]bool{},
		busy:      map[string]bool{},
		rowNote:   map[string]string{},
	}
}

// SetPalette implements PageModel.
func (p *MarketplacePage) SetPalette(pal *theme.Palette) { p.pal = pal }

// Capturing implements PageModel: plain navigation only.
func (p *MarketplacePage) Capturing() bool { return false }

// catalogURL resolves the effective catalog location: config overrides the
// built-in default.
func catalogURL() string {
	if c := config.Get(); c != nil && c.Marketplace.CatalogURL != "" {
		return c.Marketplace.CatalogURL
	}
	return market.DefaultCatalogURL
}

// RefreshCmd starts a catalog fetch unless one already ran or is running (or
// no catalog is configured). The app calls it when the settings panel opens;
// 'r' forces a re-fetch through refresh().
func (p *MarketplacePage) RefreshCmd() tea.Cmd {
	if p.catalog != nil || p.fetching || p.fetchErr != "" {
		return nil
	}
	return p.refresh()
}

// refresh unconditionally starts a fetch (nil without a configured catalog).
func (p *MarketplacePage) refresh() tea.Cmd {
	url := catalogURL()
	if url == "" || p.fetch == nil {
		return nil
	}
	p.fetching, p.fetchErr = true, ""
	fetch := p.fetch
	return func() tea.Msg {
		idx, diags, err := fetch(context.Background(), url)
		return MarketCatalogMsg{Index: idx, Diags: diags, Err: err}
	}
}

// Receive implements MsgReceiver: fetch and action results update the caches
// the view reads; every action result triggers an installed-state rescan.
func (p *MarketplacePage) Receive(msg tea.Msg) {
	switch msg := msg.(type) {
	case MarketCatalogMsg:
		p.fetching = false
		if msg.Err != nil {
			p.fetchErr = msg.Err.Error()
			return
		}
		idx := msg.Index
		p.catalog, p.diags, p.fetchErr = &idx, msg.Diags, ""
		p.rescan()
	case MarketActionMsg:
		delete(p.busy, msg.Name)
		if msg.Err != nil {
			p.rowNote[msg.Name] = msg.Err.Error()
			return
		}
		delete(p.rowNote, msg.Name)
		if msg.Action != "remove" {
			p.restart = true
		}
		p.rescan()
	}
}

// rescan refreshes the installed map from the plugins directory.
func (p *MarketplacePage) rescan() {
	if p.engine == nil {
		return
	}
	if inst, err := p.engine.Installed(); err == nil {
		p.installed = inst
	}
}

// rows returns the catalog entries sorted by name.
func (p *MarketplacePage) rows() []market.Entry {
	if p.catalog == nil {
		return nil
	}
	rows := append([]market.Entry{}, p.catalog.Plugins...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

// current returns the selected entry.
func (p *MarketplacePage) current() (market.Entry, bool) {
	rows := p.rows()
	if p.sel < 0 || p.sel >= len(rows) {
		return market.Entry{}, false
	}
	return rows[p.sel], true
}

// status classifies one entry against the installed state.
func (p *MarketplacePage) status(e market.Entry) string {
	inst, ok := p.installed[e.Name]
	switch {
	case p.busy[e.Name]:
		return "working…"
	case !ok:
		return "available"
	case market.UpdateAvailable(e, inst):
		return "update " + inst.Version.String() + " → " + e.ParsedVersion().String()
	default:
		return "installed"
	}
}

// action names what install-key would do for the entry ("" when nothing).
func (p *MarketplacePage) action(e market.Entry) string {
	inst, ok := p.installed[e.Name]
	switch {
	case p.busy[e.Name]:
		return ""
	case !ok:
		return "install"
	case market.UpdateAvailable(e, inst):
		return "update"
	default:
		return ""
	}
}

// Update implements PageModel.
func (p *MarketplacePage) Update(key tea.KeyPressMsg) tea.Cmd {
	row, hasRow := p.current()
	switch key.String() {
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "j":
		if p.sel < len(p.rows())-1 {
			p.sel++
		}
	case "enter":
		if hasRow {
			p.expanded[row.Name] = !p.expanded[row.Name]
		}
	case "i":
		// Install/update — only from the expanded detail, so the capability
		// list has been on screen (the review step).
		if hasRow && p.expanded[row.Name] && !p.busy[row.Name] {
			if act := p.action(row); act != "" {
				return p.run(row.Name, act, func(ctx context.Context) error {
					return p.engine.Install(ctx, row)
				})
			}
		}
	case "x":
		if _, ok := p.installed[row.Name]; hasRow && ok && !p.busy[row.Name] {
			return p.run(row.Name, "remove", func(context.Context) error {
				return p.engine.Remove(row.Name)
			})
		}
	case "r":
		return p.refresh()
	}
	return nil
}

// run marks the row busy and wraps one engine call into a tea.Cmd.
func (p *MarketplacePage) run(name, action string, do func(context.Context) error) tea.Cmd {
	if p.engine == nil {
		return nil
	}
	p.busy[name] = true
	delete(p.rowNote, name)
	return func() tea.Msg {
		return MarketActionMsg{Name: name, Action: action, Err: do(context.Background())}
	}
}

// View implements PageModel.
func (p *MarketplacePage) View(width, height int) string {
	pal := p.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	dim := lipgloss.NewStyle().Foreground(pal.Border)
	sel := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	warn := lipgloss.NewStyle().Foreground(pal.Error)

	var head strings.Builder
	switch {
	// A fetch that ran (or is running) outranks the URL check: an error only
	// exists because a catalog was configured at fetch time.
	case p.fetching:
		head.WriteString(dim.Render(" fetching catalog…"))
	case p.fetchErr != "":
		head.WriteString(warn.Render(" catalog: " + p.fetchErr))
	case catalogURL() == "":
		head.WriteString(dim.Render(" no catalog configured — set marketplace.catalog_url"))
	case p.catalog == nil:
		head.WriteString(dim.Render(" catalog not loaded — press r"))
	default:
		head.WriteString(dim.Render(" plugin · status · description"))
	}
	if p.restart {
		head.WriteString("\n" + warn.Render(" restart IKE to load installed/updated plugins"))
	}
	clip := lipgloss.NewStyle().MaxWidth(width)
	var list []string
	selStart, selEnd := 0, 0
	for i, e := range p.rows() {
		line := " " + padCol(e.Name, 16) + padCol(p.status(e), 22) + e.Description
		if i == p.sel {
			selStart = len(list)
			list = append(list, clip.Render(sel.Render(line)))
		} else {
			list = append(list, clip.Render(line))
		}
		if note := p.rowNote[e.Name]; note != "" {
			list = append(list, clip.Render(warn.Render("    "+note)))
		}
		if p.expanded[e.Name] {
			for _, d := range p.inspectEntry(e) {
				list = append(list, clip.Render(dim.Render("    "+d)))
			}
		}
		if i == p.sel {
			selEnd = len(list) - 1
		}
	}
	for _, d := range p.diags {
		list = append(list, clip.Render(warn.Render(" "+d)))
	}
	footer := wrapFooter([]footerLine{{
		text:  " enter details · i install/update (from details) · x remove · r refresh catalog",
		style: dim,
	}}, width, 2)
	hl := p.headLines()
	p.listH = height - hl - len(footer)
	// The list scrolls (#885): before, a MaxHeight clip made rows past the
	// window unreachable and let the selection walk off-screen.
	return head.String() + "\n" + pinFooter(list, footer, selStart, selEnd, height-hl, &p.off)
}

// headLines counts the view's lines above the first catalog row (the status
// line plus the optional restart notice) — mouse hit-testing needs the same
// arithmetic View uses (#674).
func (p *MarketplacePage) headLines() int {
	n := 1
	if p.restart {
		n++
	}
	return n
}

// Click implements the optional PageClicker seam (#674): a press on a catalog
// row (or its note / expanded detail) selects it, and a press on the
// selection toggles the detail expansion (enter semantics — install stays on
// `i`, keeping the capability-review step).
func (p *MarketplacePage) Click(_, y int) tea.Cmd {
	hl := p.headLines()
	if y < hl || (p.listH > 0 && y-hl >= p.listH) {
		return nil
	}
	// List lines shift by the scroll offset (#885).
	line := hl - p.off
	for i, e := range p.rows() {
		span := 1
		if p.rowNote[e.Name] != "" {
			span++
		}
		if p.expanded[e.Name] {
			span += len(p.inspectEntry(e))
		}
		if y >= line && y < line+span {
			if i == p.sel && y == line {
				p.expanded[e.Name] = !p.expanded[e.Name]
			} else {
				p.sel = i
			}
			return nil
		}
		line += span
	}
	return nil
}

// Wheel implements the optional PageWheeler seam (#674): moves the selection
// like j/k.
func (p *MarketplacePage) Wheel(delta int) {
	if n := len(p.rows()); n > 0 {
		p.sel = clamp(p.sel+delta, 0, n-1)
	}
}

// inspectEntry renders the expanded detail: versions, homepage and — the
// review step — the full capability list.
func (p *MarketplacePage) inspectEntry(e market.Entry) []string {
	out := []string{"version: " + e.ParsedVersion().String()}
	if inst, ok := p.installed[e.Name]; ok && inst.VersionOK {
		out[0] += "  (installed: " + inst.Version.String() + ")"
	} else if ok {
		out[0] += "  (installed: unknown version)"
	}
	if e.Homepage != "" {
		out = append(out, "homepage: "+e.Homepage)
	}
	caps := "capabilities: none — the plugin can register nothing and call no host API"
	if len(e.Capabilities) > 0 {
		caps = "capabilities: " + strings.Join(e.Capabilities, ", ")
	}
	out = append(out, caps)
	if act := p.action(e); act != "" {
		out = append(out, "press i to "+act)
	}
	return out
}

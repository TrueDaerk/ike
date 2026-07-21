package settings

import (
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/theme"
)

// plugins_page.go is the plugin manager page (Roadmap 0180, #133): every
// registered plugin with its id, live enabled state and contributed
// capabilities; e toggles a plugin by writing plugins.<id>.enabled through
// the write-back layer (the reload re-resolves the registry, keymaps and
// palette), enter expands the capability inspection. Language plugins
// (`lang-<id>` shims) show their language contribution — grammar, server
// command, install recipe — and toggling one takes its LSP server with it,
// making this page the 'LSP activation via plugin' surface. The page reads
// no state of its own: the plugin list comes from an injected closure over
// the registry (settings cannot import registry — the dependency runs the
// other way), enabled/origin come from live config.

// PluginInfo is one row's data, supplied by the injected lister.
type PluginInfo struct {
	ID            string
	Enabled       bool
	Commands      []string
	Panes         int
	Keymaps       int
	FileHandlers  int
	Hooks         int
	Themes        int
	SettingsPages int
}

// PluginsPage implements PageModel.
type PluginsPage struct {
	opts config.Options
	list func() []PluginInfo
	// onToggle builds the write-back (and any follow-up, e.g. kicking the
	// missing-server install when a language plugin turns on) for a toggle.
	onToggle func(id string, enable bool) tea.Cmd
	pal      *theme.Palette

	sel      int
	off      int // list scroll offset (#885)
	listH    int // list-window height of the last render (mouse hit-testing)
	hover    int // hovered row (-1 = none, #885)
	expanded map[string]bool
}

// NewPluginsPage builds the page from its injected seams.
func NewPluginsPage(opts config.Options, list func() []PluginInfo, onToggle func(id string, enable bool) tea.Cmd) *PluginsPage {
	return &PluginsPage{opts: opts, list: list, onToggle: onToggle, expanded: map[string]bool{}, hover: -1}
}

// SetPalette implements PageModel.
func (p *PluginsPage) SetPalette(pal *theme.Palette) { p.pal = pal }

// Capturing implements PageModel: plain navigation only.
func (p *PluginsPage) Capturing() bool { return false }

// rows returns the plugins sorted by id, language shims last-but-grouped
// (they sort naturally under "lang.").
func (p *PluginsPage) rows() []PluginInfo {
	if p.list == nil {
		return nil
	}
	rows := p.list()
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

// current returns the selected row.
func (p *PluginsPage) current() (PluginInfo, bool) {
	rows := p.rows()
	if p.sel < 0 || p.sel >= len(rows) {
		return PluginInfo{}, false
	}
	return rows[p.sel], true
}

// Update implements PageModel.
func (p *PluginsPage) Update(key tea.KeyPressMsg) tea.Cmd {
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
	case "e", " ":
		if hasRow && p.onToggle != nil {
			return p.onToggle(row.ID, !row.Enabled)
		}
	case "enter", "i":
		if hasRow {
			p.expanded[row.ID] = !p.expanded[row.ID]
		}
	}
	return nil
}

// origin reports the config layer supplying the toggle: project/user/built-in.
func (p *PluginsPage) origin(id string) string {
	switch config.Origin(p.opts, "plugins."+id+".enabled") {
	case "project":
		return "project"
	case "user":
		return "user"
	}
	return "default"
}

// langFor resolves a `lang-<id>` shim to its language registration.
func langFor(pluginID string) (lang.Language, bool) {
	id, ok := strings.CutPrefix(pluginID, "lang-")
	if !ok {
		return lang.Language{}, false
	}
	return lang.ByID(id)
}

// summary phrases a row's capability contribution: "3 commands · 1 pane" or
// the language shape for lang-* shims.
func summary(row PluginInfo) string {
	if l, ok := langFor(row.ID); ok {
		parts := []string{"language " + l.ID}
		if l.Grammar != nil {
			parts = append(parts, "grammar")
		}
		if l.Server != nil {
			parts = append(parts, "server "+l.Server.Command)
		}
		return strings.Join(parts, " · ")
	}
	var parts []string
	add := func(n int, one, many string) {
		if n == 1 {
			parts = append(parts, "1 "+one)
		} else if n > 1 {
			parts = append(parts, strconv.Itoa(n)+" "+many)
		}
	}
	add(len(row.Commands), "command", "commands")
	add(row.Panes, "pane", "panes")
	add(row.Keymaps, "keymap", "keymaps")
	add(row.FileHandlers, "file handler", "file handlers")
	add(row.Hooks, "hook", "hooks")
	add(row.Themes, "theme", "themes")
	add(row.SettingsPages, "settings page", "settings pages")
	if len(parts) == 0 {
		return "no capabilities"
	}
	return strings.Join(parts, " · ")
}

// inspect renders the expanded detail lines for a row.
func inspect(row PluginInfo) []string {
	var out []string
	if l, ok := langFor(row.ID); ok {
		out = append(out, "extensions: "+strings.Join(l.Extensions, ", "))
		if l.Server != nil {
			line := "server: " + strings.TrimSpace(l.Server.Command+" "+strings.Join(l.Server.Args, " "))
			out = append(out, line)
			if len(l.Server.Install) > 0 {
				out = append(out, "install: "+strings.Join(l.Server.Install, " "))
			}
		}
		return out
	}
	if len(row.Commands) > 0 {
		out = append(out, "commands: "+strings.Join(row.Commands, ", "))
	}
	if len(out) == 0 {
		out = append(out, "no inspectable details")
	}
	return out
}

// View implements PageModel.
func (p *PluginsPage) View(width, height int) string {
	pal := p.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	dim := lipgloss.NewStyle().Foreground(pal.Border)
	selStyle := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	offStyle := lipgloss.NewStyle().Foreground(pal.Border).Faint(true)

	clip := lipgloss.NewStyle().MaxWidth(width)
	var list []string
	selStart, selEnd := 0, 0
	for i, row := range p.rows() {
		state := "enabled"
		if !row.Enabled {
			state = "disabled"
		}
		line := " " + padCol(row.ID, 16) + padCol(state, 10) + summary(row)
		if o := p.origin(row.ID); o != "default" {
			line += dim.Render("  @" + o)
		}
		if i == p.sel {
			selStart = len(list)
		}
		switch {
		case i == p.sel:
			list = append(list, clip.Render(selStyle.Render(line)))
		case i == p.hover:
			list = append(list, clip.Render(lipgloss.NewStyle().Underline(true).Render(line)))
		case !row.Enabled:
			list = append(list, clip.Render(offStyle.Render(line)))
		default:
			list = append(list, clip.Render(line))
		}
		if p.expanded[row.ID] {
			for _, d := range inspect(row) {
				list = append(list, clip.Render(dim.Render("    "+d)))
			}
		}
		if i == p.sel {
			selEnd = len(list) - 1
		}
	}
	head := dim.Render(" plugin · state · contributes")
	footer := wrapFooter([]footerLine{{
		text:  " e toggle · enter inspect · a disabled language plugin takes its LSP server with it",
		style: dim,
	}}, width, 2)
	p.listH = height - 1 - len(footer)
	// The list scrolls (#885): before, a MaxHeight clip made rows past the
	// window unreachable and let the selection walk off-screen.
	return head + "\n" + pinFooter(list, footer, selStart, selEnd, height-1, &p.off)
}

// Click implements the optional PageClicker seam (#674): a press on a plugin
// row (or its expanded detail) selects it, and a press on the selection
// toggles the capability inspection (enter semantics).
func (p *PluginsPage) Click(_, y int) tea.Cmd {
	if y < 1 || (p.listH > 0 && y-1 >= p.listH) {
		return nil
	}
	// The header row is line 0; list lines shift by the scroll offset (#885).
	line := 1 - p.off
	for i, row := range p.rows() {
		span := 1
		if p.expanded[row.ID] {
			span += len(inspect(row))
		}
		if y >= line && y < line+span {
			if i == p.sel && y == line {
				p.expanded[row.ID] = !p.expanded[row.ID]
			} else {
				p.sel = i
			}
			return nil
		}
		line += span
	}
	return nil
}

// Hover implements the optional PageHoverer seam (#885).
func (p *PluginsPage) Hover(_, y int) {
	p.hover = -1
	if y < 1 || (p.listH > 0 && y-1 >= p.listH) {
		return
	}
	line := 1 - p.off
	for i, row := range p.rows() {
		span := 1
		if p.expanded[row.ID] {
			span += len(inspect(row))
		}
		if y >= line && y < line+span {
			p.hover = i
			return
		}
		line += span
	}
}

// Wheel implements the optional PageWheeler seam: moves the selection like
// j/k (the pinned-footer list follows it, so this also scrolls, #885).
func (p *PluginsPage) Wheel(delta int) {
	if n := len(p.rows()); n > 0 {
		p.sel = clamp(p.sel+delta, 0, n-1)
	}
}

// padCol right-pads s to width columns.
func padCol(s string, width int) string {
	if len(s) >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-len(s))
}

package settings

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/format"
	"ike/internal/theme"
)

// format_page.go is the Formatters settings page (Roadmap 0470, #1402): one
// row per language with a formatter — the plugin-declared default and/or a
// `[format.<languageID>]` config override — showing the effective command
// line, the config layer supplying it and whether the binary is on PATH.
// Since #1662 it is an interactive editor like the Language Servers page:
// `e` toggles the language's external formatting, `b` its built-in formatter,
// enter opens the override form (command / args / range_args / temp_file /
// install plus the built-in's own keys), `r` drops the override back to the
// plugin default, and `s` picks the layer the writes land in.

// FormatPage implements PageModel.
type FormatPage struct {
	navRows // last rendered height, the pgup/pgdn page (#1666)
	opts    config.Options
	pal     *theme.Palette
	host    SubPanelHost

	sel   int
	off   int
	listH int

	// scope is the layer every write of this page lands in (#1662). It starts
	// on the conventional layer for `format.*` (project) and falls back to
	// user when there is no project to write to; "s" cycles it.
	scope  config.Scope
	notice string
}

// NewFormatPage builds the page.
func NewFormatPage(opts config.Options) *FormatPage {
	scope := config.DefaultScope("format.")
	if scope == config.ProjectScope && opts.ProjectRoot == "" {
		scope = config.UserScope
	}
	return &FormatPage{opts: opts, scope: scope}
}

// SetSubPanelHost implements the hostAware injection seam (#883).
func (p *FormatPage) SetSubPanelHost(h SubPanelHost) { p.host = h }

// SetPalette implements PageModel.
func (p *FormatPage) SetPalette(pal *theme.Palette) { p.pal = pal }

// Capturing implements PageModel: the override editor is a sub-panel, so the
// page itself never captures.
func (p *FormatPage) Capturing() bool { return false }

// theme returns the active palette, defaulting when none was threaded in.
func (p *FormatPage) theme() *theme.Palette {
	if p.pal != nil {
		return p.pal
	}
	return theme.DefaultPalette()
}

// formatRow is one language's effective formatter state.
type formatRow struct {
	lang    string
	spec    format.External // effective external spec (override over default)
	def     format.External // the plugin default alone
	layer   string          // "project" / "user" / "plugin default"
	found   bool
	enabled bool
	// override reports that the language has any [format.<lang>] key set.
	override bool
	// builtin reports a registered built-in formatter, builtinOn its
	// `builtin` switch; keys are the extra config keys it reads (#1662).
	builtin   bool
	builtinOn bool
	keys      []format.ConfigKey
}

// formatOverlay returns the language's raw [format.<languageID>] config entry.
func formatOverlay(id string) map[string]any {
	if c := config.Get(); c != nil {
		return c.Format[id]
	}
	return nil
}

// rows collects the union of plugin defaults, built-in formatters and
// configured overrides.
func (p *FormatPage) rows() []formatRow {
	langs := map[string]bool{}
	for _, id := range format.ExternalDefaultLangs() {
		langs[id] = true
	}
	for _, id := range format.BuiltinLangs() {
		langs[id] = true
	}
	cfg := config.Get()
	if cfg != nil {
		for id := range cfg.Format {
			langs[id] = true
		}
	}
	ids := make([]string, 0, len(langs))
	for id := range langs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]formatRow, 0, len(ids))
	for _, id := range ids {
		row := formatRow{lang: id, layer: "plugin default", enabled: true, builtinOn: true}
		if spec, ok := format.ExternalDefault(id); ok {
			row.spec, row.def = spec, spec
		}
		row.keys, row.builtin = format.Builtin(id)
		if cfg != nil {
			if raw, ok := cfg.Format[id]; ok {
				row.override = len(raw) > 0
				if ov := format.ExternalFromConfig(raw); ov.Command != "" {
					row.spec = ov
					row.layer = p.layer(id)
				}
				if b, isBool := raw["enabled"].(bool); isBool {
					row.enabled = b
				}
				if b, isBool := raw["builtin"].(bool); isBool {
					row.builtinOn = b
				}
			}
		}
		row.found = row.spec.Ok()
		out = append(out, row)
	}
	return out
}

// current returns the selected row.
func (p *FormatPage) current() (formatRow, bool) {
	rows := p.rows()
	if p.sel < 0 || p.sel >= len(rows) {
		return formatRow{}, false
	}
	return rows[p.sel], true
}

// formatKeys are the language-table keys the page owns: the generic external
// spec plus the two switches. A language's extra keys come on top (#1662).
var formatKeys = []string{"command", "args", "range_args", "temp_file", "install", "enabled", "builtin"}

// layer reports the config layer supplying the language's override.
func (p *FormatPage) layer(id string) string {
	strongest := "user"
	for _, k := range formatKeys {
		if config.Origin(p.opts, "format."+id+"."+k) == "project" {
			return "project"
		}
	}
	return strongest
}

// scopeLabel names the layer writes land in.
func (p *FormatPage) scopeLabel() string {
	if p.scope == config.ProjectScope {
		return "project"
	}
	return "user"
}

// write persists one [format.<lang>] key at the page's write scope.
func (p *FormatPage) write(id, key string, value any) tea.Cmd {
	p.notice = ""
	return config.WriteAndReload(p.opts, p.scope, "format."+id+"."+key, value)
}

// reset removes the language's whole override — every key this page can
// write, from every addressable layer, in one batch with a single reload, so
// the row really falls back to the plugin default (#1662).
func (p *FormatPage) reset(row formatRow) tea.Cmd {
	keys := append(append([]string(nil), formatKeys...), extraKeyNames(row)...)
	var muts []config.Mutation
	for _, scope := range p.layers() {
		for _, k := range keys {
			muts = append(muts, config.Mutation{Scope: scope, Key: "format." + row.lang + "." + k, Remove: true})
		}
	}
	if len(muts) == 0 {
		p.notice = "no config layer to write to"
		return nil
	}
	p.notice = ""
	return config.ApplyAndReload(p.opts, muts)
}

// layers lists the addressable config layers — a scope without a file path
// (no project root, no home directory) would only produce write errors.
func (p *FormatPage) layers() []config.Scope {
	var out []config.Scope
	if p.opts.UserPath != "" {
		out = append(out, config.UserScope)
	}
	if p.opts.ProjectRoot != "" {
		out = append(out, config.ProjectScope)
	}
	return out
}

// extraKeyNames lists a row's language-specific config keys.
func extraKeyNames(row formatRow) []string {
	out := make([]string, 0, len(row.keys))
	for _, k := range row.keys {
		out = append(out, k.Key)
	}
	return out
}

// Update implements PageModel.
func (p *FormatPage) Update(key tea.KeyPressMsg) tea.Cmd {
	rows := p.rows()
	row, hasRow := p.current()
	if listNav(key.String(), &p.sel, len(rows), p.navPageSize()) {
		return nil
	}
	switch key.String() {
	case "space":
		// Toggle external formatting — space is the panel-wide toggle.
		if hasRow {
			return p.write(row.lang, "enabled", !row.enabled)
		}
	case "b":
		if hasRow {
			if !row.builtin {
				p.notice = row.lang + " has no built-in formatter"
				return nil
			}
			return p.write(row.lang, "builtin", !row.builtinOn)
		}
	case "enter":
		if hasRow && p.host != nil {
			p.notice = ""
			p.host.Push(newFormatForm(p, p.host, row))
		}
	case "r":
		// Reset the language's override back to the plugin default ("r" means
		// reset everywhere, #887).
		if hasRow {
			return p.reset(row)
		}
	case "s":
		// The write layer, like the panel's own scope selector (0380, #794).
		if p.scope == config.ProjectScope {
			p.scope = config.UserScope
		} else {
			p.scope = config.ProjectScope
		}
		if p.scope == config.ProjectScope && p.opts.ProjectRoot == "" {
			p.scope = config.UserScope
			p.notice = "no project layer here — writes stay in the user config"
		}
	}
	return nil
}

// formatHeadLines is the pinned header's height (mouse hit-testing, #674).
const formatHeadLines = 1

// Click implements the optional PageClicker seam (#674): a press on a row
// selects it, a press on the selection opens its override form (enter
// semantics, as on the tools and mapping pages).
func (p *FormatPage) Click(_, y int) tea.Cmd {
	idx := y - formatHeadLines
	if idx < 0 || (p.listH > 0 && idx >= p.listH) {
		return nil
	}
	idx += p.off
	rows := p.rows()
	if idx >= len(rows) {
		return nil
	}
	if idx == p.sel {
		if p.host != nil {
			p.host.Push(newFormatForm(p, p.host, rows[idx]))
		}
		return nil
	}
	p.sel = idx
	return nil
}

// Wheel implements the optional PageWheeler seam (#674): the list moves its
// selection (it follows, like j/k).
func (p *FormatPage) Wheel(delta int) {
	if n := len(p.rows()); n > 0 {
		p.sel = clamp(p.sel+delta, 0, n-1)
	}
}

// View implements PageModel.
func (p *FormatPage) View(width, height int) string {
	p.setRows(height)
	pal := p.theme()
	dim := lipgloss.NewStyle().Foreground(pal.Border)
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	selStyle := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	offStyle := lipgloss.NewStyle().Foreground(pal.Border).Faint(true)
	clip := lipgloss.NewStyle().MaxWidth(width)

	rows := p.rows()
	var list []string
	selStart, selEnd := 0, 0
	for i, row := range rows {
		cmd := strings.TrimSpace(row.spec.Command + " " + strings.Join(row.spec.Args, " "))
		if cmd == "" {
			cmd = "—"
		}
		state := "found"
		switch {
		case !row.enabled:
			state = "disabled"
		case row.spec.Command == "":
			state = "—"
		case !row.found:
			state = "missing"
		}
		line := " " + padCol(row.lang, 12) + padCol(state, 10) + cmd
		line += dim.Render("  @" + row.layer)
		if len(row.spec.RangeArgs) > 0 {
			line += dim.Render(" · range")
		}
		if row.builtin {
			builtin := " · built-in on"
			if !row.builtinOn {
				builtin = " · built-in off"
			}
			line += dim.Render(builtin)
		}
		if i == p.sel {
			selStart = len(list)
		}
		switch {
		case i == p.sel:
			list = append(list, clip.Render(selStyle.Render(line)))
		case !row.enabled || !row.found:
			list = append(list, clip.Render(offStyle.Render(line)))
		default:
			list = append(list, clip.Render(line))
		}
		if i == p.sel {
			selEnd = len(list) - 1
		}
	}
	if len(list) == 0 {
		list = append(list, dim.Render(" no formatters registered — configure one via [format.<language>]"))
	}
	head := sec.Render(" language · binary · effective command · write layer: " + p.scopeLabel())
	// The keys live on the panel's action bar; the footer carries a notice only.
	var footer []string
	if p.notice != "" {
		footer = wrapFooter([]footerLine{{text: " " + p.notice, style: lipgloss.NewStyle().Foreground(pal.Error)}}, width, 2)
	}
	p.listH = height - formatHeadLines - len(footer)
	return head + "\n" + pinFooter(list, footer, selStart, selEnd, height-formatHeadLines, &p.off)
}

// KeyHelp implements KeyHelper (#887).
// Actions lists the page's verbs for the action bar and the "?" overlay.
func (p *FormatPage) Actions() []Action {
	return []Action{
		{Key: "enter", Verb: "Edit", Hint: "the language's [format.*] override"},
		{Key: "space", Verb: "Toggle", Hint: "external formatting on/off"},
		{Key: "b", Verb: "Built-in", Hint: "built-in formatter on/off"},
		{Key: "r", Verb: "Reset", Hint: "back to the plugin default"},
		{Key: "s", Verb: "Scope: " + p.scopeLabel(), Hint: "write layer: project ↔ user"},
	}
}

// SearchItems implements Searchable (#886): one item per language row.
func (p *FormatPage) SearchItems() []SearchItem {
	var out []SearchItem
	for i, row := range p.rows() {
		i, row := i, row
		out = append(out, SearchItem{
			Label:    row.lang,
			Keywords: "formatter reformat format " + row.lang + " " + row.spec.Command,
			Activate: func() { p.sel = i },
		})
	}
	return out
}

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
// row per language with an external formatter — the plugin-declared default
// and/or a `[format.<languageID>]` config override — showing the effective
// command line, the config layer supplying it and whether the binary is on
// PATH. Read-only: overrides are written in the config file (project or
// user layer), like `[lsp.servers.*]`.

// FormatPage implements PageModel.
type FormatPage struct {
	opts config.Options
	pal  *theme.Palette

	sel   int
	off   int
	listH int
}

// NewFormatPage builds the page.
func NewFormatPage(opts config.Options) *FormatPage {
	return &FormatPage{opts: opts}
}

// SetPalette implements PageModel.
func (p *FormatPage) SetPalette(pal *theme.Palette) { p.pal = pal }

// Capturing implements PageModel: plain navigation only.
func (p *FormatPage) Capturing() bool { return false }

// formatRow is one language's effective external-formatter state.
type formatRow struct {
	lang    string
	spec    format.External
	layer   string // "project" / "user" / "plugin default"
	found   bool
	enabled bool
}

// rows collects the union of plugin defaults and configured overrides.
func (p *FormatPage) rows() []formatRow {
	langs := map[string]bool{}
	for _, id := range format.ExternalDefaultLangs() {
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
		row := formatRow{lang: id, layer: "plugin default", enabled: true}
		if spec, ok := format.ExternalDefault(id); ok {
			row.spec = spec
		}
		if cfg != nil {
			if raw, ok := cfg.Format[id]; ok {
				if ov := format.ExternalFromConfig(raw); ov.Command != "" {
					row.spec = ov
					row.layer = p.layer(id)
				}
				if b, isBool := raw["enabled"].(bool); isBool {
					row.enabled = b
				}
			}
		}
		row.found = row.spec.Ok()
		out = append(out, row)
	}
	return out
}

// layer reports the config layer supplying the language's override.
func (p *FormatPage) layer(id string) string {
	strongest := "user"
	for _, k := range []string{"command", "args", "range_args", "temp_file", "enabled"} {
		if config.Origin(p.opts, "format."+id+"."+k) == "project" {
			return "project"
		}
	}
	return strongest
}

// Update implements PageModel.
func (p *FormatPage) Update(key tea.KeyPressMsg) tea.Cmd {
	n := len(p.rows())
	if listNav(key.String(), &p.sel, n, navPage) {
		return nil
	}
	switch key.String() {
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "j":
		if p.sel < n-1 {
			p.sel++
		}
	}
	return nil
}

// View implements PageModel.
func (p *FormatPage) View(width, height int) string {
	pal := p.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	dim := lipgloss.NewStyle().Foreground(pal.Border)
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
		list = append(list, dim.Render(" no external formatters registered — configure one via [format.<language>]"))
	}
	head := dim.Render(" language · binary · effective command")
	footer := wrapFooter([]footerLine{{
		text:  " overrides live in [format.<language>] (project or user config): command, args, range_args, temp_file, enabled",
		style: dim,
	}}, width, 2)
	p.listH = height - 1 - len(footer)
	return head + "\n" + pinFooter(list, footer, selStart, selEnd, height-1, &p.off)
}

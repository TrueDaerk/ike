package settings

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// debugmap_page.go is the [[debug.php.path_mappings]] list editor (#832): a
// custom settings page managing the PHP listen mode's (#823) server↔local
// path mappings from the UI. "a" adds, enter edits, "d" deletes; writes land
// at project scope — the mapping describes this project's serving setup.

// mapFieldCount is the number of form fields: server, local.
const mapFieldCount = 2

var mapFieldNames = [mapFieldCount]string{"server", "local"}

// DebugMapPage implements PageModel.
type DebugMapPage struct {
	opts config.Options
	pal  *theme.Palette

	sel  int
	off  int // list scroll offset
	note string
	host SubPanelHost

	listH int // list-window height of the last render (mouse hit-testing)
}

// SetSubPanelHost implements the hostAware injection seam (#883).
func (t *DebugMapPage) SetSubPanelHost(h SubPanelHost) { t.host = h }

// NewDebugMapPage builds the mappings editor writing
// [[debug.php.path_mappings]] through opts.
func NewDebugMapPage(opts config.Options) *DebugMapPage {
	return &DebugMapPage{opts: opts}
}

// SetPalette implements PageModel.
func (t *DebugMapPage) SetPalette(p *theme.Palette) { t.pal = p }

// Capturing implements PageModel: the add/edit form is a sub-panel now
// (#892), so the page never captures.
func (t *DebugMapPage) Capturing() bool { return false }

// entries returns the configured mappings from the live config.
func (t *DebugMapPage) entries() []config.DebugPathMap {
	c := config.Get()
	if c == nil {
		return nil
	}
	return c.Debug.PHP.PathMappings
}

// Update implements PageModel.
func (t *DebugMapPage) Update(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "up", "k":
		if t.sel > 0 {
			t.sel--
		}
	case "down", "j":
		if t.sel < len(t.entries())-1 {
			t.sel++
		}
	case "a":
		t.openForm(-1)
	case "enter":
		if t.sel >= 0 && t.sel < len(t.entries()) {
			t.openForm(t.sel)
		}
	case "d":
		if t.sel >= 0 && t.sel < len(t.entries()) && t.host != nil {
			idx := t.sel
			e := t.entries()[idx]
			t.host.Push(newConfirm(t.host, "delete the mapping "+e.Server+" → "+e.Local, "Delete", t.pal, func() tea.Cmd {
				return t.deleteEntry(idx)
			}))
		}
	}
	return nil
}

// openForm pushes the add (idx -1) or edit form sub-panel (#892).
func (t *DebugMapPage) openForm(idx int) {
	t.note = ""
	if t.host != nil {
		t.host.Push(newDebugMapForm(t, t.host, idx))
	}
}




// deleteEntry removes the entry at idx and writes the list back.
func (t *DebugMapPage) deleteEntry(idx int) tea.Cmd {
	entries := append([]config.DebugPathMap(nil), t.entries()...)
	entries = append(entries[:idx], entries[idx+1:]...)
	if t.sel >= len(entries) && t.sel > 0 {
		t.sel--
	}
	return writeDebugMappings(t.opts, entries)
}

// writeDebugMappings persists the full list as debug.php.path_mappings at
// project scope (the tools.custom pattern: replace-by-default) and reloads.
// Shared with the app's mapping-suggestion prompt (#832).
func writeDebugMappings(opts config.Options, entries []config.DebugPathMap) tea.Cmd {
	raw := make([]map[string]any, len(entries))
	for i, e := range entries {
		raw[i] = map[string]any{"server": e.Server, "local": e.Local}
	}
	return func() tea.Msg {
		var diags []config.Diagnostic
		if err := config.WriteKey(opts, config.ProjectScope, "debug.php.path_mappings", raw); err != nil {
			diags = append(diags, config.Diagnostic{Field: "debug.php.path_mappings", Message: err.Error()})
		}
		c, loadDiags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: append(loadDiags, diags...)}
	}
}

// WriteDebugMapping appends one mapping (skipping an existing server prefix)
// and persists — the app-side seam for the #832 mapping-suggestion prompt.
func WriteDebugMapping(opts config.Options, server, local string) tea.Cmd {
	var entries []config.DebugPathMap
	if c := config.Get(); c != nil {
		entries = append(entries, c.Debug.PHP.PathMappings...)
	}
	for _, e := range entries {
		if e.Server == server {
			return nil
		}
	}
	entries = append(entries, config.DebugPathMap{Server: server, Local: local})
	return writeDebugMappings(opts, entries)
}

// pageTheme returns the active palette, defaulting when none was threaded in.
func (t *DebugMapPage) theme() *theme.Palette {
	if t.pal != nil {
		return t.pal
	}
	return theme.DefaultPalette()
}

// View implements PageModel.
func (t *DebugMapPage) View(w, h int) string {
	pal := t.theme()
	head := " server path → local path   (PHP listen-mode docroot mappings, #823)"
	entries := t.entries()
	var list []string
	for i, e := range entries {
		line := " " + pad(e.Server, 34) + "→ " + e.Local
		style := lipgloss.NewStyle()
		if i == t.sel {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(line))
	}
	if len(entries) == 0 {
		list = append(list, "no mappings configured — press a to add one (only needed when the server's docroot differs from the project layout)")
	}
	var footer []string
	{
		hint := "   a add · enter edit · d delete — local may be project-relative; applies on the next debug.listen start"
		lines := []footerLine{{text: hint, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}
		if t.note != "" {
			lines = append([]footerLine{{text: "   " + t.note, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}, lines...)
		}
		footer = wrapFooter(lines, w, 3)
	}
	headLine := lipgloss.NewStyle().Foreground(pal.Secondary).Render(head)
	t.listH = h - 1 - len(footer)
	return headLine + "\n" + pinFooter(list, footer, t.sel, t.sel, h-1, &t.off)
}


// Click implements the optional PageClicker seam (enter semantics on the
// selected row).
func (t *DebugMapPage) Click(_, y int) tea.Cmd {
	row := y - 1
	if row < 0 || (t.listH > 0 && row >= t.listH) {
		return nil
	}
	idx := row + t.off
	if idx >= len(t.entries()) {
		return nil
	}
	if idx == t.sel {
		t.openForm(idx)
		return nil
	}
	t.sel = idx
	return nil
}

// Wheel implements the optional PageWheeler seam.
func (t *DebugMapPage) Wheel(delta int) {
	if n := len(t.entries()); n > 0 {
		t.sel = clamp(t.sel+delta, 0, n-1)
	}
}

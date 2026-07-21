package settings

import (
	"strings"

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

	// The add/edit form: editIdx is the entry being edited (-1 for a new
	// one); field is the focused input, form the field values.
	editing bool
	editIdx int
	field   int
	cur     int // cursor within the focused field (#888)
	form    [mapFieldCount]string

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

// Capturing implements PageModel: while the form is open every key is field
// text — paths may contain the page's own action letters (a/d/j/k).
func (t *DebugMapPage) Capturing() bool { return t.editing }

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
	if t.editing {
		return t.updateForm(key)
	}
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

// openForm opens the add (idx -1) or edit form, seeding the fields.
func (t *DebugMapPage) openForm(idx int) {
	t.editing, t.editIdx, t.field, t.note = true, idx, 0, ""
	t.form = [mapFieldCount]string{}
	if idx >= 0 {
		e := t.entries()[idx]
		t.form = [mapFieldCount]string{e.Server, e.Local}
	}
}

// updateForm handles keys while the form is open: tab/down and shift+tab/up
// cycle fields, printable text appends, backspace edits, enter validates and
// saves, esc cancels.
func (t *DebugMapPage) updateForm(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Code == tea.KeyEscape:
		t.editing, t.note = false, ""
		return nil
	case key.Code == tea.KeyEnter:
		return t.commitForm()
	case key.Code == tea.KeyTab && key.Mod&tea.ModShift != 0, key.Code == tea.KeyUp:
		t.field = (t.field + mapFieldCount - 1) % mapFieldCount
		t.cur = len([]rune(t.form[t.field]))
	case key.Code == tea.KeyTab, key.Code == tea.KeyDown:
		t.field = (t.field + 1) % mapFieldCount
		t.cur = len([]rune(t.form[t.field]))
	default:
		// Shared cursor input (#888): rune-safe editing with word ops.
		f := newTextFieldAt(t.form[t.field], t.cur)
		if handled, _ := f.Handle(key); handled {
			t.form[t.field], t.cur = f.text, f.cur
		}
	}
	return nil
}

// validate checks the form; "" means valid. self is the index being edited
// (-1 for add) so its own server prefix is not a duplicate of itself.
func (t *DebugMapPage) validate(self int) string {
	server := strings.TrimSpace(t.form[0])
	if server == "" {
		return "server path is required"
	}
	if strings.TrimSpace(t.form[1]) == "" {
		return "local path is required"
	}
	for i, e := range t.entries() {
		if i != self && e.Server == server {
			return "a mapping for " + server + " already exists"
		}
	}
	return ""
}

// commitForm validates and writes the whole list back at project scope.
func (t *DebugMapPage) commitForm() tea.Cmd {
	if msg := t.validate(t.editIdx); msg != "" {
		t.note = msg
		return nil
	}
	entry := config.DebugPathMap{
		Server: strings.TrimSpace(t.form[0]),
		Local:  strings.TrimSpace(t.form[1]),
	}
	entries := append([]config.DebugPathMap(nil), t.entries()...)
	if t.editIdx >= 0 && t.editIdx < len(entries) {
		entries[t.editIdx] = entry
	} else {
		entries = append(entries, entry)
	}
	t.editing, t.note = false, ""
	return writeDebugMappings(t.opts, entries)
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
		if i == t.sel && !t.editing {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(line))
	}
	if len(entries) == 0 {
		list = append(list, "no mappings configured — press a to add one (only needed when the server's docroot differs from the project layout)")
	}
	var footer []string
	if t.editing {
		footer = t.formFooter(w)
	} else {
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

// formFooter renders the add/edit form pinned under the list.
func (t *DebugMapPage) formFooter(w int) []string {
	pal := t.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	verb := "edit mapping"
	if t.editIdx < 0 {
		verb = "new mapping"
	}
	lines := []footerLine{{text: "   " + verb + ":", style: sec}}
	for i, name := range mapFieldNames {
		marker := "  "
		style := lipgloss.NewStyle()
		text := t.form[i]
		if i == t.field {
			marker = "> "
			style = style.Bold(true)
			text = newTextFieldAt(t.form[i], t.cur).View()
		}
		lines = append(lines, footerLine{
			text:  "   " + marker + pad(name, 8) + text,
			style: style,
		})
	}
	if t.note != "" {
		lines = append(lines, footerLine{text: "   ✗ " + t.note, style: lipgloss.NewStyle().Foreground(pal.Error)})
	}
	lines = append(lines, footerLine{text: "   tab next field · enter saves · esc cancels", style: sec})
	return wrapFooter(lines, w, len(lines))
}

// Click implements the optional PageClicker seam (enter semantics on the
// selected row; a press while the form is open cancels it).
func (t *DebugMapPage) Click(_, y int) tea.Cmd {
	if t.editing {
		t.editing, t.note = false, ""
		return nil
	}
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
	if t.editing {
		return
	}
	if n := len(t.entries()); n > 0 {
		t.sel = clamp(t.sel+delta, 0, n-1)
	}
}

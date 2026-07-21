package settings

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
	"ike/internal/toolcatalog"
)

// tools_page.go is the [[tools.custom]] list editor (#755): a custom settings
// page managing the custom TUI tool panes (#741) from the UI. The list shows
// the configured entries; "a" adds, enter edits, "d" deletes, "s" opens the
// curated suggestions (#759) — common TUIs from the tool catalog, added with
// one keypress and installed when their binary is missing. Edits go
// through the write-back layer at user scope and reload through the normal
// pipeline, so the tool.<name> palette commands re-shape live.

// toolFieldCount is the number of form fields: name, command, args, cwd,
// placement, multiple (#835).
const toolFieldCount = 6

var toolFieldNames = [toolFieldCount]string{"name", "command", "args", "cwd", "placement", "multiple"}

// ToolsPage implements PageModel.
type ToolsPage struct {
	opts config.Options
	pal  *theme.Palette

	sel  int
	off  int // list scroll offset
	note string

	// The add/edit form: editIdx is the entry being edited (-1 for a new
	// one); field is the focused input, form the field values.
	editing bool
	editIdx int
	field   int
	form    [toolFieldCount]string

	// The suggestion picker (#759): catalog entries not yet configured.
	suggesting bool
	sugSel     int

	listH int // list-window height of the last render (mouse hit-testing)
}

// NewToolsPage builds the tools editor writing [[tools.custom]] through opts.
func NewToolsPage(opts config.Options) *ToolsPage {
	return &ToolsPage{opts: opts}
}

// SetPalette implements PageModel.
func (t *ToolsPage) SetPalette(p *theme.Palette) { t.pal = p }

// Capturing implements PageModel: while the form is open every key is field
// text — names may contain the page's own action letters (a/d/j/k) — and the
// suggestion picker owns esc/enter the same way.
func (t *ToolsPage) Capturing() bool { return t.editing || t.suggesting }

// entries returns the configured tools from the live config.
func (t *ToolsPage) entries() []config.ToolEntry {
	c := config.Get()
	if c == nil {
		return nil
	}
	return c.Tools.Custom
}

// suggestionCatalog lists the offerable catalog entries; a seam for tests.
var suggestionCatalog = toolcatalog.Offered

// suggestions returns the catalog entries not yet configured.
func (t *ToolsPage) suggestions() []toolcatalog.Entry {
	configured := map[string]bool{}
	for _, e := range t.entries() {
		configured[e.Name] = true
	}
	var out []toolcatalog.Entry
	for _, e := range suggestionCatalog() {
		if !configured[e.Name] {
			out = append(out, e)
		}
	}
	return out
}

// Update implements PageModel.
func (t *ToolsPage) Update(key tea.KeyPressMsg) tea.Cmd {
	if t.editing {
		return t.updateForm(key)
	}
	if t.suggesting {
		return t.updateSuggest(key)
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
		if t.sel >= 0 && t.sel < len(t.entries()) {
			return t.deleteEntry(t.sel)
		}
	case "s":
		if len(t.suggestions()) == 0 {
			t.note = "no suggestions — every catalog tool is already configured"
			return nil
		}
		t.suggesting, t.sugSel, t.note = true, 0, ""
	}
	return nil
}

// updateSuggest handles keys while the suggestion picker is open: j/k move,
// enter adds the highlighted tool (writing the config entry and installing
// the binary when missing), esc returns to the list.
func (t *ToolsPage) updateSuggest(key tea.KeyPressMsg) tea.Cmd {
	sugs := t.suggestions()
	switch key.String() {
	case "up", "k":
		if t.sugSel > 0 {
			t.sugSel--
		}
	case "down", "j":
		if t.sugSel < len(sugs)-1 {
			t.sugSel++
		}
	case "enter":
		if t.sugSel < 0 || t.sugSel >= len(sugs) {
			return nil
		}
		return t.addSuggestion(sugs[t.sugSel])
	case "esc":
		t.suggesting = false
	}
	return nil
}

// addSuggestion writes the catalog entry into tools.custom and kicks the
// install when the binary is missing; the install result surfaces as a toast
// (toolcatalog.InstallResultMsg). The picker closes — the new entry appears
// in the list as soon as the reload lands.
func (t *ToolsPage) addSuggestion(e toolcatalog.Entry) tea.Cmd {
	entries := append([]config.ToolEntry(nil), t.entries()...)
	entries = append(entries, config.ToolEntry{
		Name:      e.Name,
		Command:   e.Command,
		Args:      e.Args,
		Placement: e.Placement,
	})
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	t.suggesting = false
	write := t.writeEntries(entries)
	if e.Installed() {
		return write
	}
	return tea.Batch(write, toolcatalog.Install(e))
}

// openForm opens the add (idx -1) or edit form, seeding the fields.
func (t *ToolsPage) openForm(idx int) {
	t.editing, t.editIdx, t.field, t.note = true, idx, 0, ""
	t.form = [toolFieldCount]string{}
	if idx >= 0 {
		e := t.entries()[idx]
		multiple := ""
		if e.Multiple {
			multiple = "true"
		}
		t.form = [toolFieldCount]string{e.Name, e.Command, strings.Join(e.Args, " "), e.Cwd, e.Placement, multiple}
	}
}

// updateForm handles keys while the form is open: tab/down and shift+tab/up
// cycle fields, printable text appends, backspace edits, enter validates and
// saves, esc cancels.
func (t *ToolsPage) updateForm(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Code == tea.KeyEscape:
		t.editing, t.note = false, ""
		return nil
	case key.Code == tea.KeyEnter:
		return t.commitForm()
	case key.Code == tea.KeyTab && key.Mod&tea.ModShift != 0, key.Code == tea.KeyUp:
		t.field = (t.field + toolFieldCount - 1) % toolFieldCount
	case key.Code == tea.KeyTab, key.Code == tea.KeyDown:
		t.field = (t.field + 1) % toolFieldCount
	case key.Code == tea.KeyBackspace:
		if f := t.form[t.field]; f != "" {
			t.form[t.field] = f[:len(f)-1]
		}
	default:
		if key.Text != "" {
			t.form[t.field] += key.Text
		}
	}
	return nil
}

// validate checks the form; "" means valid. self is the index being edited
// (-1 for add) so its own name is not a duplicate of itself.
func (t *ToolsPage) validate(self int) string {
	name := strings.TrimSpace(t.form[0])
	if name == "" {
		return "name is required"
	}
	if strings.TrimSpace(t.form[1]) == "" {
		return "command is required"
	}
	switch t.form[4] {
	case "", "bottom", "right":
	default:
		return "placement must be bottom or right"
	}
	switch t.form[5] {
	case "", "true", "false":
	default:
		return "multiple must be true or false"
	}
	for i, e := range t.entries() {
		if i != self && e.Name == name {
			return "a tool named " + name + " already exists"
		}
	}
	return ""
}

// commitForm validates and writes the whole [[tools.custom]] list back at
// user scope, reloading the config so the palette commands re-shape.
func (t *ToolsPage) commitForm() tea.Cmd {
	if msg := t.validate(t.editIdx); msg != "" {
		t.note = msg
		return nil
	}
	entry := config.ToolEntry{
		Name:      strings.TrimSpace(t.form[0]),
		Command:   strings.TrimSpace(t.form[1]),
		Args:      strings.Fields(t.form[2]),
		Cwd:       strings.TrimSpace(t.form[3]),
		Placement: t.form[4],
		Multiple:  t.form[5] == "true",
	}
	entries := append([]config.ToolEntry(nil), t.entries()...)
	if t.editIdx >= 0 && t.editIdx < len(entries) {
		entries[t.editIdx] = entry
	} else {
		entries = append(entries, entry)
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	}
	t.editing, t.note = false, ""
	return t.writeEntries(entries)
}

// deleteEntry removes the entry at idx and writes the list back.
func (t *ToolsPage) deleteEntry(idx int) tea.Cmd {
	entries := append([]config.ToolEntry(nil), t.entries()...)
	entries = append(entries[:idx], entries[idx+1:]...)
	if t.sel >= len(entries) && t.sel > 0 {
		t.sel--
	}
	return t.writeEntries(entries)
}

// writeEntries persists the full list as tools.custom at user scope (the
// project.history pattern: the list is replace-by-default) and reloads.
func (t *ToolsPage) writeEntries(entries []config.ToolEntry) tea.Cmd {
	opts := t.opts
	raw := make([]map[string]any, len(entries))
	for i, e := range entries {
		m := map[string]any{"name": e.Name, "command": e.Command}
		if len(e.Args) > 0 {
			m["args"] = e.Args
		}
		if e.Cwd != "" {
			m["cwd"] = e.Cwd
		}
		if e.Placement != "" {
			m["placement"] = e.Placement
		}
		if e.Multiple {
			m["multiple"] = true
		}
		raw[i] = m
	}
	return func() tea.Msg {
		var diags []config.Diagnostic
		if err := config.WriteKey(opts, config.UserScope, "tools.custom", raw); err != nil {
			diags = append(diags, config.Diagnostic{Field: "tools.custom", Message: err.Error()})
		}
		c, loadDiags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: append(loadDiags, diags...)}
	}
}

// theme returns the active palette, defaulting when none was threaded in.
func (t *ToolsPage) theme() *theme.Palette {
	if t.pal != nil {
		return t.pal
	}
	return theme.DefaultPalette()
}

// View implements PageModel.
func (t *ToolsPage) View(w, h int) string {
	if t.suggesting {
		return t.viewSuggestions(w, h)
	}
	pal := t.theme()
	head := " name · command · placement   (custom TUI tool panes, #741)"
	entries := t.entries()
	var list []string
	for i, e := range entries {
		line := " " + pad(e.Name, 18) + pad(e.Command+argSuffix(e.Args), 34) + placementLabel(e.Placement)
		if e.Multiple {
			line += " · multi"
		}
		style := lipgloss.NewStyle()
		if i == t.sel && !t.editing {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(line))
	}
	if len(entries) == 0 {
		list = append(list, "no tools configured — press a to add one")
	}
	var footer []string
	if t.editing {
		footer = t.formFooter(w)
	} else {
		hint := "   a add · enter edit · d delete · s suggestions — each tool is a tool.<name> palette command"
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

// viewSuggestions renders the suggestion picker (#759): the catalog entries
// not yet configured, each with its install state.
func (t *ToolsPage) viewSuggestions(w, h int) string {
	pal := t.theme()
	head := lipgloss.NewStyle().Foreground(pal.Secondary).
		Render(" suggested tools — enter adds the entry and installs a missing binary")
	sugs := t.suggestions()
	var list []string
	for i, e := range sugs {
		state := "installs via …"
		switch {
		case e.Installed():
			state = "installed"
		default:
			if argv, ok := e.InstallArgv(); ok {
				state = "installs via " + strings.Join(argv, " ")
			} else {
				state = "no installer found"
			}
		}
		line := " " + pad(e.Name, 14) + pad(e.Description, 52) + state
		style := lipgloss.NewStyle()
		if i == t.sugSel {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(line))
	}
	footer := wrapFooter([]footerLine{{
		text:  "   j/k move · enter add · esc back",
		style: lipgloss.NewStyle().Foreground(pal.Secondary),
	}}, w, 2)
	t.listH = h - 1 - len(footer)
	var off int
	return head + "\n" + pinFooter(list, footer, t.sugSel, t.sugSel, h-1, &off)
}

// formFooter renders the add/edit form pinned under the list: one line per
// field, the focused one carrying the cursor, plus the hint/validation line.
func (t *ToolsPage) formFooter(w int) []string {
	pal := t.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	verb := "edit tool"
	if t.editIdx < 0 {
		verb = "new tool"
	}
	lines := []footerLine{{text: "   " + verb + ":", style: sec}}
	for i, name := range toolFieldNames {
		marker, cursor := "  ", ""
		style := lipgloss.NewStyle()
		if i == t.field {
			marker, cursor = "> ", "▌"
			style = style.Bold(true)
		}
		lines = append(lines, footerLine{
			text:  "   " + marker + pad(name, 10) + t.form[i] + cursor,
			style: style,
		})
	}
	hint := "   tab next field · enter saves · esc cancels"
	if t.note != "" {
		lines = append(lines, footerLine{text: "   ✗ " + t.note, style: lipgloss.NewStyle().Foreground(pal.Error)})
	}
	lines = append(lines, footerLine{text: hint, style: sec})
	return wrapFooter(lines, w, len(lines))
}

// argSuffix renders the args for the list line, "" when none.
func argSuffix(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return " " + strings.Join(args, " ")
}

// placementLabel names the split zone, defaulting like the open path does.
func placementLabel(p string) string {
	if p == "" {
		return "bottom"
	}
	return p
}

// Click implements the optional PageClicker seam: a press on a row selects
// it, a press on the selected row opens the edit form (enter semantics); a
// press while the form is open cancels it.
func (t *ToolsPage) Click(_, y int) tea.Cmd {
	if t.editing || t.suggesting {
		t.editing, t.suggesting, t.note = false, false, ""
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

// Wheel implements the optional PageWheeler seam: the list moves its
// selection; inert while the form is open.
func (t *ToolsPage) Wheel(delta int) {
	if t.editing || t.suggesting {
		return
	}
	if n := len(t.entries()); n > 0 {
		t.sel = clamp(t.sel+delta, 0, n-1)
	}
}

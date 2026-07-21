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

// ToolsPage implements PageModel. The add/edit form runs as a SubPanel
// (#883, tools_form.go) pushed through host.
type ToolsPage struct {
	opts config.Options
	pal  *theme.Palette
	host SubPanelHost

	sel  int
	off  int // list scroll offset
	note string

	// The suggestion picker (#759): catalog entries not yet configured.
	suggesting bool
	sugSel     int

	listH int // list-window height of the last render (mouse hit-testing)
}

// SetSubPanelHost implements the hostAware injection seam (#883).
func (t *ToolsPage) SetSubPanelHost(h SubPanelHost) { t.host = h }

// NewToolsPage builds the tools editor writing [[tools.custom]] through opts.
func NewToolsPage(opts config.Options) *ToolsPage {
	return &ToolsPage{opts: opts}
}

// SetPalette implements PageModel.
func (t *ToolsPage) SetPalette(p *theme.Palette) { t.pal = p }

// Capturing implements PageModel: the suggestion picker owns esc/enter; the
// add/edit form is a SubPanel now (#883) and captures at the panel level.
func (t *ToolsPage) Capturing() bool { return t.suggesting }

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
	if t.suggesting {
		return t.updateSuggest(key)
	}
	if listNav(key.String(), &t.sel, len(t.entries())+1, navPage) {
		return nil
	}
	switch key.String() {
	case "a":
		t.openForm(-1)
	case "enter":
		if t.sel == len(t.entries()) {
			// The trailing "+ Suggestions…" action row (#887).
			return t.openSuggestions()
		}
		if t.sel >= 0 && t.sel < len(t.entries()) {
			t.openForm(t.sel)
		}
	case "d":
		if t.sel >= 0 && t.sel < len(t.entries()) && t.host != nil {
			idx, name := t.sel, t.entries()[t.sel].Name
			t.host.Push(newConfirm(t.host, "delete the tool "+name, "Delete", t.pal, func() tea.Cmd {
				return t.deleteEntry(idx)
			}))
		}
	case "s":
		return t.openSuggestions()
	}
	return nil
}

// openSuggestions opens the curated-suggestions picker (#759); reachable via
// the visible action row and the "s" shortcut.
func (t *ToolsPage) openSuggestions() tea.Cmd {
	if len(t.suggestions()) == 0 {
		t.note = "no suggestions — every catalog tool is already configured"
		return nil
	}
	t.suggesting, t.sugSel, t.note = true, 0, ""
	return nil
}

// KeyHelp implements KeyHelper (#887).
func (t *ToolsPage) KeyHelp() []string {
	return []string{
		"a  add a tool · enter  edit the selected tool (or open the action row)",
		"d  delete · s  curated suggestions",
	}
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

// openForm pushes the add (idx -1) or edit form sub-panel (#883).
func (t *ToolsPage) openForm(idx int) {
	t.note = ""
	if t.host != nil {
		t.host.Push(newToolForm(t, t.host, idx))
	}
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
		if i == t.sel {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(line))
	}
	if len(entries) == 0 {
		list = append(list, "no tools configured — press a to add one")
	}
	// The trailing suggestions action row (#887): a visible entry point, not
	// just the "s" letter.
	{
		label := " + Suggestions…"
		style := lipgloss.NewStyle().Foreground(pal.Info)
		if t.sel == len(entries) {
			style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(label))
	}
	hint := "   a add · enter edit · d delete · s suggestions — each tool is a tool.<name> palette command"
	lines2 := []footerLine{{text: hint, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}
	if t.note != "" {
		lines2 = append([]footerLine{{text: "   " + t.note, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}, lines2...)
	}
	footer := wrapFooter(lines2, w, 3)
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
// it, a press on the selected row opens the edit form sub-panel (enter
// semantics); a press while the suggestion picker is open closes it.
func (t *ToolsPage) Click(_, y int) tea.Cmd {
	if t.suggesting {
		t.suggesting, t.note = false, ""
		return nil
	}
	row := y - 1
	if row < 0 || (t.listH > 0 && row >= t.listH) {
		return nil
	}
	idx := row + t.off
	if idx > len(t.entries()) {
		return nil
	}
	if idx == t.sel {
		if idx == len(t.entries()) {
			return t.openSuggestions()
		}
		t.openForm(idx)
		return nil
	}
	t.sel = idx
	return nil
}

// Wheel implements the optional PageWheeler seam: the list moves its
// selection; inert while the suggestion picker is open.
func (t *ToolsPage) Wheel(delta int) {
	if t.suggesting {
		return
	}
	if n := len(t.entries()) + 1; n > 0 { // + the suggestions action row
		t.sel = clamp(t.sel+delta, 0, n-1)
	}
}

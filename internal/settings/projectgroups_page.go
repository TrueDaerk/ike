package settings

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// projectgroups_page.go is the [[project.groups]] list editor (Epic 0510,
// #2573): the tools_page.go shape — a list where "a" adds, enter edits and "d"
// deletes behind the shared confirm (#891) — over the project groups the data
// layer (#2570) persists. "o" opens the selected group, the page's one verb
// that leaves the panel.
//
// Writes go through the write-back layer at *user* scope only, with no scope
// toggle: a group spans projects on this machine, so a per-project copy would
// fracture it (internal/project/group.go says the same about its own writers).
// The normal reload re-shapes the group picker live.
//
// The project layer reaches the page as injected functions (ProjectGroupOps)
// rather than an import: internal/project imports the palette, which imports
// the registry, which imports this package — importing it back would close the
// cycle. The app wires the real project.* functions in one place
// (projectGroupOps, internal/app/app.go).

// ProjectGroupsPageTitle is the page's rail title, shared with the group table
// (groups.go) and the app's registration.
const ProjectGroupsPageTitle = "Project Groups"

// ProjectGroupOps is the project-layer seam the page and its form need. Every
// field is required in production; a nil one degrades to a message instead of
// a panic, so a partially wired page still explains itself.
type ProjectGroupOps struct {
	// ResolveRoot expands and checks one root as typed — `~`, an absolute
	// path, or a name relative to the project directory — and returns it
	// absolute (project.ValidateGroupRoot).
	ResolveRoot func(text string) (string, error)
	// Validate runs the group rules over the live config and returns the
	// normalised name and roots (project.ValidateGroup).
	Validate func(name string, roots []string) (string, []string, error)
	// Write persists the whole list at user scope (project.WriteGroups).
	Write func(groups []config.ProjectGroup) error
	// Remove deletes one group by name, clearing the active-group marker with
	// it (project.RemoveGroup).
	Remove func(name string) error
	// Compact shortens a root for the list column (project.CompactPath).
	Compact func(path string) string
}

// ProjectGroupsPage implements PageModel. The add/edit form runs as a SubPanel
// (#883, projectgroups_form.go).
type ProjectGroupsPage struct {
	navRows // last rendered height, the pgup/pgdn page (#1666)
	opts    config.Options
	ops     ProjectGroupOps
	pal     *theme.Palette
	host    SubPanelHost

	sel  int
	off  int // list scroll offset
	note string

	listH int // list-window height of the last render (mouse hit-testing)

	// open dispatches project.group.open for a group name (#2571). The app
	// injects it; without it "o" only reports that nothing is wired up.
	open func(name string) tea.Cmd
}

// NewProjectGroupsPage builds the groups editor writing [[project.groups]]
// through opts, with ops supplying the project layer.
func NewProjectGroupsPage(opts config.Options, ops ProjectGroupOps) *ProjectGroupsPage {
	return &ProjectGroupsPage{opts: opts, ops: ops}
}

// SetSubPanelHost implements the hostAware injection seam (#883).
func (p *ProjectGroupsPage) SetSubPanelHost(h SubPanelHost) { p.host = h }

// SetPalette implements PageModel.
func (p *ProjectGroupsPage) SetPalette(pal *theme.Palette) { p.pal = pal }

// Capturing implements PageModel: the add/edit form is a sub-panel, so the
// page never captures.
func (p *ProjectGroupsPage) Capturing() bool { return false }

// SetGroupOpen injects the project.group.open dispatcher for the "o" verb
// (#2571); the app supplies it at construction.
func (p *ProjectGroupsPage) SetGroupOpen(fn func(name string) tea.Cmd) { p.open = fn }

// entries returns the stored groups from the live config, in list order.
func (p *ProjectGroupsPage) entries() []config.ProjectGroup {
	c := config.Get()
	if c == nil {
		return nil
	}
	return c.Project.Groups
}

// Update implements PageModel.
func (p *ProjectGroupsPage) Update(key tea.KeyPressMsg) tea.Cmd {
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if listNav(key.String(), &p.sel, len(p.entries()), p.navPageSize()) {
		return nil
	}
	// Shared add·edit·delete actions (#2466).
	if pageActionKey(key.String(), pageActions{
		host: p.host, pal: p.pal, sel: p.sel, n: len(p.entries()),
		open: p.openForm,
		confirm: func(idx int) string {
			return "delete the group " + p.entries()[idx].Name
		},
		remove: p.deleteEntry,
	}) {
		return nil
	}
	if key.String() == "o" {
		return p.openSelected()
	}
	return nil
}

// openSelected dispatches project.group.open for the selected row.
func (p *ProjectGroupsPage) openSelected() tea.Cmd {
	if p.sel < 0 || p.sel >= len(p.entries()) {
		return nil
	}
	name := p.entries()[p.sel].Name
	if p.open == nil {
		p.note = "opening a group is not available in this build"
		return nil
	}
	p.note = ""
	return p.open(name)
}

// openForm pushes the add (idx -1) or edit form sub-panel (#883).
func (p *ProjectGroupsPage) openForm(idx int) {
	p.note = ""
	if p.host != nil {
		p.host.Push(newGroupForm(p, p.host, idx))
	}
}

// deleteEntry removes the group at idx and writes the list back.
func (p *ProjectGroupsPage) deleteEntry(idx int) tea.Cmd {
	groups := p.entries()
	if idx < 0 || idx >= len(groups) {
		return nil
	}
	name := groups[idx].Name
	if p.sel >= len(groups)-1 && p.sel > 0 {
		p.sel--
	}
	if p.ops.Remove == nil {
		p.note = "deleting a group is not available in this build"
		return nil
	}
	// Remove owns the marker rule: deleting the active group clears
	// project.active_group too.
	return p.writeCmd(func() error { return p.ops.Remove(name) })
}

// writeEntries persists the full list at user scope and reloads — the path
// add and edit take, so a rename keeps the group's list position.
func (p *ProjectGroupsPage) writeEntries(groups []config.ProjectGroup) tea.Cmd {
	if p.ops.Write == nil {
		p.note = "saving a group is not available in this build"
		return nil
	}
	return p.writeCmd(func() error { return p.ops.Write(groups) })
}

// writeCmd runs one persisting action and reloads the config, reporting a
// write failure as a project.groups diagnostic (the tools.custom pattern).
func (p *ProjectGroupsPage) writeCmd(do func() error) tea.Cmd {
	opts := p.opts
	return func() tea.Msg {
		var diags []config.Diagnostic
		if err := do(); err != nil {
			diags = append(diags, config.Diagnostic{Field: "project.groups", Message: err.Error()})
		}
		c, loadDiags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: append(loadDiags, diags...)}
	}
}

// theme returns the active palette, defaulting when none was threaded in.
func (p *ProjectGroupsPage) theme() *theme.Palette {
	if p.pal != nil {
		return p.pal
	}
	return theme.DefaultPalette()
}

// compact shortens a root for display; identity without the seam.
func (p *ProjectGroupsPage) compact(path string) string {
	if p.ops.Compact == nil {
		return path
	}
	return p.ops.Compact(path)
}

// rootSummary renders a group's root column: the member count plus the first
// root, compacted so a deep path cannot push the line off screen.
func (p *ProjectGroupsPage) rootSummary(g config.ProjectGroup) string {
	n := strconv.Itoa(len(g.Roots)) + " root"
	if len(g.Roots) != 1 {
		n += "s"
	}
	if len(g.Roots) == 0 {
		return n
	}
	return n + " · " + p.compact(g.Roots[0])
}

// View implements PageModel.
func (p *ProjectGroupsPage) View(w, h int) string {
	p.setRows(h)
	pal := p.theme()
	head := " name · roots   (named sets of project roots opened as one)"
	groups := p.entries()
	var list []string
	for i, g := range groups {
		line := " " + pad(g.Name, 20) + p.rootSummary(g)
		style := lipgloss.NewStyle()
		if i == p.sel {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(line))
	}
	if len(groups) == 0 {
		list = append(list, "no project groups — press a to add one (a name plus the project roots it opens)")
	}
	hint := "   groups are stored for your user, never per project · o opens the selected group"
	lines := []footerLine{{text: hint, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}
	if p.note != "" {
		lines = append([]footerLine{{text: "   " + p.note, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}, lines...)
	}
	footer := wrapFooter(lines, w, 3)
	headLine := lipgloss.NewStyle().Foreground(pal.Secondary).Render(head)
	p.listH = h - 1 - len(footer)
	return headLine + "\n" + pinFooter(list, footer, p.sel, p.sel, h-1, &p.off)
}

// Click implements the optional PageClicker seam (enter semantics on the
// selected row).
func (p *ProjectGroupsPage) Click(_, y int) tea.Cmd {
	return pageClick(y, p.off, p.listH, len(p.entries()), &p.sel, p.openForm)
}

// Wheel implements the optional PageWheeler seam.
func (p *ProjectGroupsPage) Wheel(delta int) {
	if n := len(p.entries()); n > 0 {
		p.sel = clamp(p.sel+delta, 0, n-1)
	}
}

// Actions lists the page's verbs for the action bar and the "?" overlay.
func (p *ProjectGroupsPage) Actions() []Action {
	inRange := func() bool { return p.sel >= 0 && p.sel < len(p.entries()) }
	return []Action{
		{Key: "a", Verb: "Add", Hint: "a project group"},
		{Key: "enter", Verb: "Edit"},
		{Key: "d", Verb: "Delete", Enabled: inRange},
		{Key: "o", Verb: "Open", Hint: "the selected group", Enabled: inRange},
	}
}

// KeyHelp adds the notes the keys do not carry.
func (p *ProjectGroupsPage) KeyHelp() []string {
	return []string{
		"a group is a name plus its member roots — opening one parks every member",
		"groups always persist at user scope: a group spans projects",
	}
}

// trimAll trims every element and drops the empty ones, preserving order.
func trimAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

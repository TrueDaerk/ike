package settings

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// es_page.go is the [[elasticsearch.endpoints]] list editor (#1927): a custom
// settings page managing the ES console's cluster endpoints from the UI. The
// list shows the configured entries with their auth scheme; "a" adds, enter
// edits, "d" deletes, "s" toggles the write scope between user and project —
// unlike tools the endpoint list is often project-specific (the cluster
// belongs to the codebase), so the target is a visible choice rather than a
// hard-coded scope. Edits go through the write-back layer and reload through
// the normal pipeline, so the es.console.<name> palette commands re-shape
// live.

// esFieldCount is the number of form fields: name, url, username, password,
// api key. Auth is one of two schemes (basic vs. api key), enforced by the
// form's validation rather than a widget.
const esFieldCount = 5

var esFieldNames = [esFieldCount]string{"name", "url", "username", "password", "api key"}

// ESPage implements PageModel. The add/edit form runs as a SubPanel (#883,
// es_form.go) pushed through host.
type ESPage struct {
	navRows // last rendered height, the pgup/pgdn page (#1666)
	opts    config.Options
	pal     *theme.Palette
	host    SubPanelHost

	sel   int
	off   int // list scroll offset
	note  string
	scope config.Scope // write target; the zero value is config.UserScope

	listH int // list-window height of the last render (mouse hit-testing)
}

// SetSubPanelHost implements the hostAware injection seam (#883).
func (t *ESPage) SetSubPanelHost(h SubPanelHost) { t.host = h }

// NewESPage builds the endpoints editor writing [[elasticsearch.endpoints]]
// through opts.
func NewESPage(opts config.Options) *ESPage {
	return &ESPage{opts: opts}
}

// SetPalette implements PageModel.
func (t *ESPage) SetPalette(p *theme.Palette) { t.pal = p }

// Capturing implements PageModel: the add/edit form is a SubPanel (#883) and
// captures at the panel level, so the page never captures.
func (t *ESPage) Capturing() bool { return false }

// entries returns the configured endpoints from the live config.
func (t *ESPage) entries() []config.ESEndpoint {
	c := config.Get()
	if c == nil {
		return nil
	}
	return c.Elasticsearch.Endpoints
}

// Update implements PageModel.
func (t *ESPage) Update(key tea.KeyPressMsg) tea.Cmd {
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if listNav(key.String(), &t.sel, len(t.entries()), t.navPageSize()) {
		return nil
	}
	switch key.String() {
	case "a":
		t.openForm(-1)
	case "enter":
		if t.sel >= 0 && t.sel < len(t.entries()) {
			t.openForm(t.sel)
		}
	case "d":
		if t.sel >= 0 && t.sel < len(t.entries()) && t.host != nil {
			idx, name := t.sel, t.entries()[t.sel].Name
			t.host.Push(newConfirm(t.host, "delete the endpoint "+name, "Delete", t.pal, func() tea.Cmd {
				return t.deleteEntry(idx)
			}))
		}
	case "s":
		// Flip the write target. Only future writes are affected — nothing is
		// moved between layers, mirroring how the schema pages pick a scope.
		if t.scope == config.UserScope {
			t.scope = config.ProjectScope
		} else {
			t.scope = config.UserScope
		}
	}
	return nil
}

// KeyHelp implements KeyHelper (#887).
func (t *ESPage) KeyHelp() []string {
	return []string{
		"a  add an endpoint · enter  edit the selected endpoint",
		"d  delete · s  scope: " + esScopeLabel(t.scope),
	}
}

// openForm pushes the add (idx -1) or edit form sub-panel (#883).
func (t *ESPage) openForm(idx int) {
	t.note = ""
	if t.host != nil {
		t.host.Push(newESForm(t, t.host, idx))
	}
}

// deleteEntry removes the entry at idx and writes the list back.
func (t *ESPage) deleteEntry(idx int) tea.Cmd {
	entries := append([]config.ESEndpoint(nil), t.entries()...)
	entries = append(entries[:idx], entries[idx+1:]...)
	if t.sel >= len(entries) && t.sel > 0 {
		t.sel--
	}
	return t.writeEntries(entries)
}

// writeEntries persists the full list as elasticsearch.endpoints at the
// page's current scope (the tools.custom pattern: replace-by-default) and
// reloads. Optional auth fields are only written when set, so the TOML stays
// as terse as a hand-written entry.
func (t *ESPage) writeEntries(entries []config.ESEndpoint) tea.Cmd {
	opts, scope := t.opts, t.scope
	raw := make([]map[string]any, len(entries))
	for i, e := range entries {
		m := map[string]any{"name": e.Name, "url": e.URL}
		if e.Username != "" {
			m["username"] = e.Username
		}
		if e.Password != "" {
			m["password"] = e.Password
		}
		if e.APIKey != "" {
			m["api_key"] = e.APIKey
		}
		raw[i] = m
	}
	return func() tea.Msg {
		var diags []config.Diagnostic
		if err := config.WriteKey(opts, scope, "elasticsearch.endpoints", raw); err != nil {
			diags = append(diags, config.Diagnostic{Field: "elasticsearch.endpoints", Message: err.Error()})
		}
		c, loadDiags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: append(loadDiags, diags...)}
	}
}

// theme returns the active palette, defaulting when none was threaded in.
func (t *ESPage) theme() *theme.Palette {
	if t.pal != nil {
		return t.pal
	}
	return theme.DefaultPalette()
}

// esAuthBadge names the endpoint's auth scheme for the list line, "" when the
// cluster is open. Validation keeps the schemes exclusive, so Username wins
// should a hand-edited file carry both.
func esAuthBadge(e config.ESEndpoint) string {
	switch {
	case e.Username != "":
		return "basic"
	case e.APIKey != "":
		return "api-key"
	}
	return ""
}

// esScopeLabel renders the write target for the footer and key help.
func esScopeLabel(s config.Scope) string {
	if s == config.ProjectScope {
		return "project"
	}
	return "user"
}

// View implements PageModel.
func (t *ESPage) View(w, h int) string {
	t.setRows(h)
	pal := t.theme()
	head := " name · url   (Elasticsearch console endpoints, #1927)"
	entries := t.entries()
	var list []string
	for i, e := range entries {
		line := " " + pad(e.Name, 18) + pad(e.URL, 40)
		if badge := esAuthBadge(e); badge != "" {
			line += " · " + badge
		}
		style := lipgloss.NewStyle()
		if i == t.sel {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(line))
	}
	if len(entries) == 0 {
		list = append(list, "no endpoints configured — press a to add one")
	}
	hint := "   a add · enter edit · d delete · s scope: " + esScopeLabel(t.scope) +
		" — each endpoint is an es.console.<name> palette command"
	lines := []footerLine{{text: hint, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}
	if t.note != "" {
		lines = append([]footerLine{{text: "   " + t.note, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}, lines...)
	}
	footer := wrapFooter(lines, w, 3)
	headLine := lipgloss.NewStyle().Foreground(pal.Secondary).Render(head)
	t.listH = h - 1 - len(footer)
	return headLine + "\n" + pinFooter(list, footer, t.sel, t.sel, h-1, &t.off)
}

// Click implements the optional PageClicker seam: a press on a row selects
// it, a press on the selected row opens the edit form sub-panel (enter
// semantics).
func (t *ESPage) Click(_, y int) tea.Cmd {
	return pageClick(y, t.off, t.listH, len(t.entries()), &t.sel, t.openForm)
}

// Wheel implements the optional PageWheeler seam.
func (t *ESPage) Wheel(delta int) {
	if n := len(t.entries()); n > 0 {
		t.sel = clamp(t.sel+delta, 0, n-1)
	}
}

package settings

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/secret"
	"ike/internal/theme"
)

// httpvars_page.go is the [[http.variables]] list editor: the global .http
// variables every request file can reference as {{name}}. Both scopes are
// listed together, project rows first since they win; "a" adds to the scope
// "s" selects, enter edits a row in its own scope, "d" deletes it. Values the
// secret-masking key patterns classify as secrets render as the editor's
// mask.

// HTTPVarsPageTitle is the page's rail title; the HTTP Variables palette
// (#3122) opens the panel on it for a global variable.
const HTTPVarsPageTitle = "HTTP Variables"

// httpVarFieldCount is the number of form fields: name, value.
const httpVarFieldCount = 2

var httpVarFieldNames = [httpVarFieldCount]string{"name", "value"}

// HTTPVarsPage implements PageModel. The add/edit form runs as a SubPanel
// (httpvars_form.go) pushed through host.
type HTTPVarsPage struct {
	navRows
	opts config.Options
	pal  *theme.Palette
	host SubPanelHost

	sel   int
	off   int
	scope config.Scope // scope new variables are added to

	listH int // list-window height of the last render (mouse hit-testing)
}

// NewHTTPVarsPage builds the global .http variables editor writing
// [[http.variables]] through opts.
func NewHTTPVarsPage(opts config.Options) *HTTPVarsPage {
	return &HTTPVarsPage{opts: opts}
}

// SetSubPanelHost implements the hostAware injection seam.
func (t *HTTPVarsPage) SetSubPanelHost(h SubPanelHost) { t.host = h }

// SetPalette implements PageModel.
func (t *HTTPVarsPage) SetPalette(p *theme.Palette) { t.pal = p }

// Capturing implements PageModel: the form captures at the panel level.
func (t *HTTPVarsPage) Capturing() bool { return false }

// entries returns the variables of both scopes from the live config, project
// entries first.
func (t *HTTPVarsPage) entries() []config.HTTPVariable {
	c := config.Get()
	if c == nil {
		return nil
	}
	return c.HTTP.Variables
}

// scopeEntries returns the variables of one scope, in file order.
func (t *HTTPVarsPage) scopeEntries(scope config.Scope) []config.HTTPVariable {
	var out []config.HTTPVariable
	for _, v := range t.entries() {
		if v.Scope == scope {
			out = append(out, v)
		}
	}
	return out
}

// Update implements PageModel.
func (t *HTTPVarsPage) Update(key tea.KeyPressMsg) tea.Cmd {
	if listNav(key.String(), &t.sel, len(t.entries()), t.navPageSize()) {
		return nil
	}
	if pageActionKey(key.String(), pageActions{
		host: t.host, pal: t.pal, sel: t.sel, n: len(t.entries()),
		open: t.openForm,
		confirm: func(idx int) string {
			v := t.entries()[idx]
			return "delete the " + esScopeLabel(v.Scope) + " variable " + v.Name
		},
		remove: t.deleteEntry,
	}) {
		return nil
	}
	if key.String() == "s" {
		if t.scope == config.UserScope {
			t.scope = config.ProjectScope
		} else {
			t.scope = config.UserScope
		}
	}
	return nil
}

// Actions lists the page's verbs for the action bar and the "?" overlay.
func (t *HTTPVarsPage) Actions() []Action {
	return []Action{
		{Key: "a", Verb: "Add", Hint: "a variable"},
		{Key: "enter", Verb: "Edit"},
		{Key: "d", Verb: "Delete"},
		{Key: "s", Verb: "Scope: " + esScopeLabel(t.scope), Hint: "the scope new variables are added to"},
	}
}

// openForm pushes the add (idx -1) or edit form sub-panel.
func (t *HTTPVarsPage) openForm(idx int) {
	if t.host != nil {
		t.host.Push(newHTTPVarForm(t, t.host, idx))
	}
}

// deleteEntry removes the entry at idx from its scope and writes that scope
// back.
func (t *HTTPVarsPage) deleteEntry(idx int) tea.Cmd {
	entries := t.entries()
	if idx < 0 || idx >= len(entries) {
		return nil
	}
	target := entries[idx]
	var kept []config.HTTPVariable
	for i, v := range entries {
		if i != idx && v.Scope == target.Scope {
			kept = append(kept, v)
		}
	}
	if t.sel >= len(entries)-1 && t.sel > 0 {
		t.sel--
	}
	return t.writeScope(target.Scope, kept)
}

// writeScope persists the full variable list of one scope and reloads; an
// empty list removes the key from that layer.
func (t *HTTPVarsPage) writeScope(scope config.Scope, vars []config.HTTPVariable) tea.Cmd {
	opts := t.opts
	raw := make([]map[string]any, len(vars))
	for i, v := range vars {
		raw[i] = map[string]any{"name": v.Name, "value": v.Value}
	}
	return func() tea.Msg {
		var err error
		if len(raw) == 0 {
			err = config.RemoveKey(opts, scope, "http.variables")
		} else {
			err = config.WriteKey(opts, scope, "http.variables", raw)
		}
		var diags []config.Diagnostic
		if err != nil {
			diags = append(diags, config.Diagnostic{Field: "http.variables", Message: err.Error()})
		}
		c, loadDiags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: append(loadDiags, diags...)}
	}
}

// httpVarMasked reports whether a variable's value renders masked: secret
// masking is on and its name is secret-suspect.
func httpVarMasked(name string) bool {
	c := config.Get()
	return c != nil && c.Editor.SecretMasking && secret.Suspect(name)
}

// httpVarShadowed reports whether the user variable at idx is overridden by
// a project variable of the same name.
func httpVarShadowed(entries []config.HTTPVariable, idx int) bool {
	v := entries[idx]
	if v.Scope != config.UserScope {
		return false
	}
	for _, o := range entries {
		if o.Scope == config.ProjectScope && o.Name == v.Name {
			return true
		}
	}
	return false
}

func (t *HTTPVarsPage) theme() *theme.Palette {
	if t.pal != nil {
		return t.pal
	}
	return theme.DefaultPalette()
}

// View implements PageModel.
func (t *HTTPVarsPage) View(w, h int) string {
	t.setRows(h)
	pal := t.theme()
	head := " scope    name · value   (global .http variables)"
	entries := t.entries()
	var list []string
	for i, v := range entries {
		value := v.Value
		if httpVarMasked(v.Name) {
			value = secret.Mask
		}
		line := " " + pad(esScopeLabel(v.Scope), 9) + pad(v.Name, 24) + value
		if httpVarShadowed(entries, i) {
			line += " · overridden by project"
		}
		style := lipgloss.NewStyle()
		if i == t.sel {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(line))
	}
	if len(entries) == 0 {
		list = append(list, "no variables defined — press a to add one")
	}
	hint := "   adds to the " + esScopeLabel(t.scope) + " scope — {{name}} in any .http file resolves here when the file, env file and process environment do not define it"
	footer := wrapFooter([]footerLine{{text: hint, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}, w, 3)
	headLine := lipgloss.NewStyle().Foreground(pal.Secondary).Render(head)
	t.listH = h - 1 - len(footer)
	return headLine + "\n" + pinFooter(list, footer, t.sel, t.sel, h-1, &t.off)
}

// Click implements PageClicker: a press on a row selects it, a press on the
// selected row opens the edit form.
func (t *HTTPVarsPage) Click(_, y int) tea.Cmd {
	return pageClick(y, t.off, t.listH, len(t.entries()), &t.sel, t.openForm)
}

// Wheel implements PageWheeler.
func (t *HTTPVarsPage) Wheel(delta int) {
	if n := len(t.entries()); n > 0 {
		t.sel = clamp(t.sel+delta, 0, n-1)
	}
}

package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// httpvars_form.go is the add/edit form of a global .http variable as a
// SubPanel: name and value rows, Save and Cancel buttons, esc pops back to
// the list. The name check is the config validator's (config.
// HTTPVariableNameError), so the form rejects exactly what loading would
// drop.

// httpVarForm implements SubPanel.
type httpVarForm struct {
	page  *HTTPVarsPage
	host  SubPanelHost
	idx   int          // entry being edited, -1 for a new one
	scope config.Scope // scope the entry is written to

	fieldNav
	form [httpVarFieldCount]string
	note string
}

// newHTTPVarForm seeds the form from the entry at idx (-1 = blank, added to
// the page's scope).
func newHTTPVarForm(page *HTTPVarsPage, host SubPanelHost, idx int) *httpVarForm {
	f := &httpVarForm{page: page, host: host, idx: idx, scope: page.scope}
	f.fieldNav = newFieldNav(httpVarFieldCount, func(i int) string { return f.form[i] })
	if entries := page.entries(); idx >= 0 && idx < len(entries) {
		e := entries[idx]
		f.form = [httpVarFieldCount]string{e.Name, e.Value}
		f.scope = e.Scope
	}
	return f
}

// Title implements SubPanel.
func (f *httpVarForm) Title() string {
	if f.idx < 0 {
		return "New Variable"
	}
	return "Edit Variable"
}

// Capturing implements SubPanel: every key is field text.
func (f *httpVarForm) Capturing() bool { return true }

// Buttons implements SubPanel.
func (f *httpVarForm) Buttons() []Button {
	return []Button{
		{Label: "Save", Do: f.save},
		{Label: "Cancel", Do: func() tea.Cmd { f.host.Pop(); return nil }},
	}
}

// Update implements SubPanel.
func (f *httpVarForm) Update(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Code == tea.KeyEscape:
		f.host.Pop()
	case key.Code == tea.KeyEnter:
		return f.save()
	case f.fieldNav.Update(key):
	default:
		tf := newTextFieldAt(f.form[f.field], f.cur)
		if handled, _ := tf.Handle(key); handled {
			f.form[f.field], f.cur = tf.Text, tf.Cur
		}
	}
	return nil
}

// Click implements SubPanelClicker: a press on a field row focuses it.
func (f *httpVarForm) Click(_, y int) tea.Cmd {
	if y >= 0 && y < httpVarFieldCount {
		f.Focus(y)
	}
	return nil
}

// View implements SubPanel: one row per field, then the validation/hint
// line. The value renders as bullets while the name is secret-suspect; the
// mask keeps the rune count, so the cursor indexes it like the real text.
func (f *httpVarForm) View(w, h int) string {
	pal := f.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := make([]string, 0, h)
	masked := httpVarMasked(strings.TrimSpace(f.form[0]))
	for i, name := range httpVarFieldNames {
		marker := "  "
		style := lipgloss.NewStyle()
		text := f.form[i]
		if i == 1 && masked {
			text = strings.Repeat("•", len([]rune(text)))
		}
		if i == f.field {
			marker = "▸ "
			style = style.Bold(true)
			text = newTextFieldAt(text, f.cur).View()
		}
		lines = append(lines, clip.Render(style.Render(" "+marker+pad(name, 8)+text)))
	}
	lines = append(lines, clip.Render(sec.Render("   scope   "+esScopeLabel(f.scope))))
	lines = append(lines, "")
	if f.note != "" {
		lines = append(lines, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+f.note)))
	} else {
		lines = append(lines, clip.Render(sec.Render(" tab next field · enter saves · esc cancels · {{other}} in a value expands")))
	}
	return strings.Join(lines, "\n")
}

// save validates and writes the entry's scope; success pops back to the list.
func (f *httpVarForm) save() tea.Cmd {
	if msg := f.validate(); msg != "" {
		f.note = msg
		return nil
	}
	entry := config.HTTPVariable{Name: strings.TrimSpace(f.form[0]), Value: strings.TrimSpace(f.form[1]), Scope: f.scope}
	var vars []config.HTTPVariable
	replaced := false
	for i, v := range f.page.entries() {
		if v.Scope != f.scope {
			continue
		}
		if i == f.idx {
			v, replaced = entry, true
		}
		vars = append(vars, v)
	}
	if !replaced {
		vars = append(vars, entry)
	}
	f.host.Pop()
	return f.page.writeScope(f.scope, vars)
}

// validate checks the form; "" means valid.
func (f *httpVarForm) validate() string {
	name := strings.TrimSpace(f.form[0])
	if msg := config.HTTPVariableNameError(name); msg != "" {
		return msg
	}
	for i, v := range f.page.entries() {
		if i != f.idx && v.Scope == f.scope && v.Name == name {
			return "a " + esScopeLabel(f.scope) + " variable named " + name + " already exists"
		}
	}
	return ""
}

func (f *httpVarForm) theme() *theme.Palette {
	if f.page.pal != nil {
		return f.page.pal
	}
	return theme.DefaultPalette()
}

// Paste inserts a pasted block into the focused field at its cursor.
func (f *httpVarForm) Paste(text string) bool {
	tf := newTextFieldAt(f.form[f.field], f.cur)
	if !tf.Paste(text) {
		return false
	}
	f.form[f.field], f.cur = tf.Text, tf.Cur
	return true
}

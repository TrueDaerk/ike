package settings

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
)

// lsp_override.go is the Language Servers page's override editor: one form
// for a server's command, args and options JSON, opened with enter. The
// three used to be three letters (c, a, o) opening three single-field
// forms; one form behind the row's primary key is what every other list page
// does, and it frees the letters for their panel-wide meanings.

// lspOverrideForm edits every [lsp.servers.<id>] override of one server.
type lspOverrideForm struct {
	page *LSPPage
	host SubPanelHost
	lang string

	fieldNav // focused field + cursor within it (#888, #2466)
	form     [lspFieldCount]string
	initial  [lspFieldCount]string
	note     string
}

const lspFieldCount = 3

// The field order is the order of the form rows; the index doubles as the
// config key selector in commit.
var lspFieldNames = [lspFieldCount]string{"command", "args", "options"}

// newLSPOverrideForm seeds the form with the server's effective values.
func newLSPOverrideForm(page *LSPPage, host SubPanelHost, lang string) *lspOverrideForm {
	f := &lspOverrideForm{page: page, host: host, lang: lang}
	f.fieldNav = newFieldNav(lspFieldCount, func(i int) string { return f.form[i] })
	if l, ok := page.langByID(lang); ok {
		cmd, args := effective(l)
		f.form = [lspFieldCount]string{cmd, strings.Join(args, " "), settingsJSON(lang)}
	}
	f.initial = f.form
	return f
}

func (f *lspOverrideForm) Title() string   { return "Edit " + f.lang + " overrides" }
func (f *lspOverrideForm) Capturing() bool { return true }

func (f *lspOverrideForm) Buttons() []Button {
	return []Button{
		{Label: "Save", Do: f.commit},
		{Label: "Cancel", Do: func() tea.Cmd { f.host.Pop(); return nil }},
	}
}

func (f *lspOverrideForm) Update(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Code == tea.KeyEscape:
		f.host.Pop()
	case key.Code == tea.KeyEnter:
		return f.commit()
	case f.fieldNav.Update(key):
	default:
		tf := newTextFieldAt(f.form[f.field], f.cur)
		if handled, _ := tf.Handle(key); handled {
			f.form[f.field], f.cur = tf.Text, tf.Cur
		}
	}
	return nil
}

// Paste inserts into the focused field at its cursor (#2002).
func (f *lspOverrideForm) Paste(text string) bool {
	tf := newTextFieldAt(f.form[f.field], f.cur)
	if !tf.Paste(text) {
		return false
	}
	f.form[f.field], f.cur = tf.Text, tf.Cur
	return true
}

// Click implements SubPanelClicker: a press on a field row focuses it.
func (f *lspOverrideForm) Click(_, y int) tea.Cmd {
	if y >= 0 && y < lspFieldCount {
		f.field = y
	}
	return nil
}

// commit validates the options JSON and writes every field that changed
// in one batch with a single reload; an emptied field removes its override.
func (f *lspOverrideForm) commit() tea.Cmd {
	options := strings.TrimSpace(f.form[2])
	var optMap map[string]any
	if options != "" {
		if err := json.Unmarshal([]byte(options), &optMap); err != nil || !json.Valid([]byte(options)) {
			f.note = "options must be a JSON object"
			return nil
		}
	}
	var muts []config.Mutation
	for i, name := range lspFieldNames {
		if f.form[i] == f.initial[i] {
			continue
		}
		key := "lsp.servers." + f.lang + "." + map[string]string{"command": "command", "args": "args", "options": "settings"}[name]
		raw := strings.TrimSpace(f.form[i])
		if raw == "" {
			muts = append(muts, config.Mutation{Scope: config.ProjectScope, Key: key, Remove: true})
			continue
		}
		var value any = raw
		switch name {
		case "args":
			value = strings.Fields(raw)
		case "options":
			value = optMap
		}
		muts = append(muts, config.Mutation{Scope: config.ProjectScope, Key: key, Value: value})
	}
	f.page.invalid = ""
	f.host.Pop()
	if len(muts) == 0 {
		return nil
	}
	return config.ApplyAndReload(f.page.opts, muts)
}

func (f *lspOverrideForm) View(w, h int) string {
	pal := f.page.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := []string{clip.Render(sec.Render(" " + f.lang + " · project layer · empty = plugin default"))}
	for i, name := range lspFieldNames {
		marker := "  "
		style := lipgloss.NewStyle()
		text := f.form[i]
		if i == f.field {
			marker = "▸ "
			style = style.Bold(true)
			text = newTextFieldAt(f.form[i], f.cur).View()
		}
		lines = append(lines, clip.Render(style.Render(" "+marker+pad(name, 9)+text)))
	}
	lines = append(lines, "")
	if f.note != "" {
		lines = append(lines, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+f.note)))
	} else {
		lines = append(lines, clip.Render(sec.Render(" args are space-separated · options is a JSON object · tab next field · enter saves")))
	}
	return strings.Join(lines, "\n")
}

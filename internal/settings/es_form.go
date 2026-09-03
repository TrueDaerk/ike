package settings

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// es_form.go is the endpoint add/edit form as a SubPanel (#883, the toolForm
// pattern): fields are click-to-focus rows, Save and Cancel are buttons, esc
// pops back to the list without destroying the page state. The form is the
// strict half of the shared URL check (config.ESURLError): where the config
// validator drops a bad entry with a diagnostic, the form rejects the input
// outright with the same message.

// esForm implements SubPanel.
type esForm struct {
	page *ESPage
	host SubPanelHost
	idx  int // entry being edited, -1 for a new one

	fieldNav // focused field + cursor within it (#888, #2466)
	form     [esFieldCount]string
	note     string
}

// newESForm seeds the form from the entry at idx (-1 = blank).
func newESForm(page *ESPage, host SubPanelHost, idx int) *esForm {
	f := &esForm{page: page, host: host, idx: idx}
	f.fieldNav = newFieldNav(esFieldCount, func(i int) string { return f.form[i] })
	if idx >= 0 {
		e := page.entries()[idx]
		f.form = [esFieldCount]string{e.Name, e.URL, e.Username, e.Password, e.APIKey}
	}
	return f
}

// Title implements SubPanel (the breadcrumb segment).
func (f *esForm) Title() string {
	if f.idx < 0 {
		return "New Endpoint"
	}
	return "Edit Endpoint"
}

// Capturing implements SubPanel: every key is field text (URLs and secrets may
// contain any letter), so the form owns esc/enter itself.
func (f *esForm) Capturing() bool { return true }

// Buttons implements SubPanel: click-only here (the form captures keys); the
// key equivalents are handled in Update and shown in the hint line.
func (f *esForm) Buttons() []Button {
	return []Button{
		{Label: "Save", Do: f.save},
		{Label: "Cancel", Do: func() tea.Cmd { f.host.Pop(); return nil }},
	}
}

// Update implements SubPanel.
func (f *esForm) Update(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Code == tea.KeyEscape:
		f.host.Pop()
	case key.Code == tea.KeyEnter:
		return f.save()
	case f.fieldNav.Update(key): // shared field motion (#2466)
	default:
		// Shared cursor input (#888).
		tf := newTextFieldAt(f.form[f.field], f.cur)
		if handled, _ := tf.Handle(key); handled {
			f.form[f.field], f.cur = tf.Text, tf.Cur
		}
	}
	return nil
}

// Click implements SubPanelClicker: a press on a field row focuses it.
func (f *esForm) Click(_, y int) tea.Cmd {
	if y >= 0 && y < esFieldCount {
		f.field = y
	}
	return nil
}

// esMaskedField marks the secret fields (password, api key) whose values are
// rendered masked.
func esMaskedField(i int) bool { return i == 3 || i == 4 }

// View implements SubPanel: one row per field, the focused one carrying the
// cursor, then the validation/hint line. The secret fields render as bullets:
// the mask keeps the rune count, so the shared cursor (#888) indexes the
// masked string exactly like the real one.
func (f *esForm) View(w, h int) string {
	pal := f.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := make([]string, 0, h)
	for i, name := range esFieldNames {
		marker := "  "
		style := lipgloss.NewStyle()
		text := f.form[i]
		if esMaskedField(i) {
			text = strings.Repeat("•", len([]rune(text)))
		}
		if i == f.field {
			marker = "▸ "
			style = style.Bold(true)
			text = newTextFieldAt(text, f.cur).View()
		}
		lines = append(lines, clip.Render(style.Render(" "+marker+pad(name, 10)+text)))
	}
	lines = append(lines, "")
	if f.note != "" {
		lines = append(lines, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+f.note)))
	} else {
		lines = append(lines, clip.Render(sec.Render(" tab next field · enter saves · esc cancels")))
	}
	return strings.Join(lines, "\n")
}

// save validates and writes the entry; success pops back to the list.
func (f *esForm) save() tea.Cmd {
	if msg := f.validate(); msg != "" {
		f.note = msg
		return nil
	}
	entry := config.ESEndpoint{
		Name:     strings.TrimSpace(f.form[0]),
		URL:      strings.TrimSpace(f.form[1]),
		Username: strings.TrimSpace(f.form[2]),
		Password: strings.TrimSpace(f.form[3]),
		APIKey:   strings.TrimSpace(f.form[4]),
	}
	entries := append([]config.ESEndpoint(nil), f.page.entries()...)
	if f.idx >= 0 && f.idx < len(entries) {
		entries[f.idx] = entry
	} else {
		entries = append(entries, entry)
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	}
	f.host.Pop()
	return f.page.writeEntries(entries)
}

// validate checks the form; "" means valid. The rules mirror the lenient
// config validator (internal/config/validate.go) but reject instead of
// downgrading: a both-schemes entry there loses its api_key silently, here it
// never saves in the first place.
func (f *esForm) validate() string {
	name := strings.TrimSpace(f.form[0])
	if name == "" {
		return "name is required"
	}
	for i, e := range f.page.entries() {
		if i != f.idx && e.Name == name {
			return "an endpoint named " + name + " already exists"
		}
	}
	// The shared URL check (#1927), verbatim: the same message a broken
	// hand-edited entry would produce as a diagnostic.
	if msg := config.ESURLError(f.form[1]); msg != "" {
		return msg
	}
	user, pass, key := strings.TrimSpace(f.form[2]), strings.TrimSpace(f.form[3]), strings.TrimSpace(f.form[4])
	if key != "" && (user != "" || pass != "") {
		return "basic auth and api key are mutually exclusive"
	}
	if pass != "" && user == "" {
		return "password needs a username"
	}
	return ""
}

func (f *esForm) theme() *theme.Palette {
	if f.page.pal != nil {
		return f.page.pal
	}
	return theme.DefaultPalette()
}

// Paste inserts a pasted block into the focused field at its cursor (#2002),
// through the same shared helper the typed keys use.
func (f *esForm) Paste(text string) bool {
	tf := newTextFieldAt(f.form[f.field], f.cur)
	if !tf.Paste(text) {
		return false
	}
	f.form[f.field], f.cur = tf.Text, tf.Cur
	return true
}

package settings

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// tools_form.go is the tools add/edit form as a SubPanel (#883) — the proof
// migration off the pinned-footer form: fields are click-to-focus rows, Save
// and Cancel are buttons, esc pops back to the list without destroying the
// page state, and a stray click no longer discards typed input.

// toolForm implements SubPanel.
type toolForm struct {
	page *ToolsPage
	host SubPanelHost
	idx  int // entry being edited, -1 for a new one

	field int
	cur   int // cursor within the focused field (#888)
	form  [toolFieldCount]string
	note  string
}

// newToolForm seeds the form from the entry at idx (-1 = blank).
func newToolForm(page *ToolsPage, host SubPanelHost, idx int) *toolForm {
	f := &toolForm{page: page, host: host, idx: idx}
	if idx >= 0 {
		e := page.entries()[idx]
		multiple := ""
		if e.Multiple {
			multiple = "true"
		}
		f.form = [toolFieldCount]string{e.Name, e.Command, strings.Join(e.Args, " "), e.Cwd, e.Placement, multiple}
	}
	return f
}

// Title implements SubPanel (the breadcrumb segment).
func (f *toolForm) Title() string {
	if f.idx < 0 {
		return "New Tool"
	}
	return "Edit Tool"
}

// Capturing implements SubPanel: every key is field text (names may contain
// any letter), so the form owns esc/enter itself.
func (f *toolForm) Capturing() bool { return true }

// Buttons implements SubPanel: click-only here (the form captures keys); the
// key equivalents are handled in Update and shown in the hint line.
func (f *toolForm) Buttons() []Button {
	return []Button{
		{Label: "Save", Do: f.save},
		{Label: "Cancel", Do: func() tea.Cmd { f.host.Pop(); return nil }},
	}
}

// Update implements SubPanel.
func (f *toolForm) Update(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Code == tea.KeyEscape:
		f.host.Pop()
	case key.Code == tea.KeyEnter:
		return f.save()
	case key.Code == tea.KeyTab && key.Mod&tea.ModShift != 0, key.Code == tea.KeyUp:
		f.field = (f.field + toolFieldCount - 1) % toolFieldCount
		f.cur = len([]rune(f.form[f.field]))
	case key.Code == tea.KeyTab, key.Code == tea.KeyDown:
		f.field = (f.field + 1) % toolFieldCount
		f.cur = len([]rune(f.form[f.field]))
	default:
		// Shared cursor input (#888).
		tf := newTextFieldAt(f.form[f.field], f.cur)
		if handled, _ := tf.Handle(key); handled {
			f.form[f.field], f.cur = tf.text, tf.cur
		}
	}
	return nil
}

// Click implements SubPanelClicker: a press on a field row focuses it.
func (f *toolForm) Click(_, y int) tea.Cmd {
	if y >= 0 && y < toolFieldCount {
		f.field = y
	}
	return nil
}

// View implements SubPanel: one row per field, the focused one carrying the
// cursor, then the validation/hint line.
func (f *toolForm) View(w, h int) string {
	pal := f.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := make([]string, 0, h)
	for i, name := range toolFieldNames {
		marker := "  "
		style := lipgloss.NewStyle()
		text := f.form[i]
		if i == f.field {
			marker = "▸ "
			style = style.Bold(true)
			text = newTextFieldAt(f.form[i], f.cur).View()
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
func (f *toolForm) save() tea.Cmd {
	if msg := f.validate(); msg != "" {
		f.note = msg
		return nil
	}
	entry := config.ToolEntry{
		Name:      strings.TrimSpace(f.form[0]),
		Command:   strings.TrimSpace(f.form[1]),
		Args:      strings.Fields(f.form[2]),
		Cwd:       strings.TrimSpace(f.form[3]),
		Placement: f.form[4],
		Multiple:  f.form[5] == "true",
	}
	entries := append([]config.ToolEntry(nil), f.page.entries()...)
	if f.idx >= 0 && f.idx < len(entries) {
		entries[f.idx] = entry
	} else {
		entries = append(entries, entry)
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	}
	f.host.Pop()
	return f.page.writeEntries(entries)
}

// validate checks the form; "" means valid.
func (f *toolForm) validate() string {
	name := strings.TrimSpace(f.form[0])
	if name == "" {
		return "name is required"
	}
	if strings.TrimSpace(f.form[1]) == "" {
		return "command is required"
	}
	switch f.form[4] {
	case "", "bottom", "right":
	default:
		return "placement must be bottom or right"
	}
	switch f.form[5] {
	case "", "true", "false":
	default:
		return "multiple must be true or false"
	}
	for i, e := range f.page.entries() {
		if i != f.idx && e.Name == name {
			return "a tool named " + name + " already exists"
		}
	}
	return ""
}

func (f *toolForm) theme() *theme.Palette {
	if f.page.pal != nil {
		return f.page.pal
	}
	return theme.DefaultPalette()
}

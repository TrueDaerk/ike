package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/format"
	"ike/internal/theme"
)

// format_form.go is the Formatters page's per-language override editor
// (#1662): one sub-panel form over the `[format.<languageID>]` table —
// command (with path completion), args, range_args, temp_file, install, plus
// the keys a built-in formatter declares (SQL's `keywords`). The form starts
// on the *effective* values, and a field left equal to the plugin default (or
// emptied) removes its key instead of freezing the default into the config —
// so an override file only ever holds what actually differs.

// formatFieldKind is how one form field maps onto a TOML value.
type formatFieldKind int

const (
	fmtText formatFieldKind = iota // plain string
	fmtList                        // space-separated argument list
	fmtBool                        // true / false
	fmtEnum                        // one of a declared value set
)

// formatField describes one editable key of a language's format table.
type formatField struct {
	key    string
	kind   formatFieldKind
	values []string // fmtEnum: the accepted values
	def    string   // the value in effect while the key is absent
	help   string
}

// formatFields builds the field list for a row: the generic external-command
// keys, then whatever the language's built-in formatter declares.
func formatFields(row formatRow) []formatField {
	fields := []formatField{
		{key: "command", kind: fmtText, def: row.def.Command, help: "binary probed on PATH"},
		{key: "args", kind: fmtList, def: strings.Join(row.def.Args, " "), help: "whole-file arguments, space-separated"},
		{key: "range_args", kind: fmtList, def: strings.Join(row.def.RangeArgs, " "), help: "opts into Reformat Selection"},
		{key: "temp_file", kind: fmtBool, def: boolText(row.def.TempFile), help: "true for tools that cannot read stdin"},
		{key: "install", kind: fmtText, def: row.def.Install, help: "install hint shown when the binary is missing"},
	}
	for _, k := range row.keys {
		f := formatField{key: k.Key, kind: fmtText, def: k.Default, help: k.Help}
		if len(k.Values) > 0 {
			f.kind, f.values = fmtEnum, k.Values
		}
		fields = append(fields, f)
	}
	return fields
}

// boolText renders a bool the way the form edits it.
func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// formatForm implements SubPanel.
type formatForm struct {
	page   *FormatPage
	host   SubPanelHost
	row    formatRow
	fields []formatField

	values  []string
	field   int
	cur     int
	suggest pathSuggest
	note    string
}

// newFormatForm seeds the form from the language's effective values: the
// configured override where it sets a key, the plugin default otherwise.
func newFormatForm(page *FormatPage, host SubPanelHost, row formatRow) *formatForm {
	f := &formatForm{page: page, host: host, row: row, fields: formatFields(row)}
	raw := formatOverlay(row.lang)
	f.values = make([]string, len(f.fields))
	for i, field := range f.fields {
		f.values[i] = field.def
		v, ok := raw[field.key]
		if !ok {
			continue
		}
		switch field.kind {
		case fmtList:
			if list := configArgs(v); list != nil {
				f.values[i] = strings.Join(list, " ")
			}
		case fmtBool:
			if b, isBool := v.(bool); isBool {
				f.values[i] = boolText(b)
			}
		default:
			if s, isStr := v.(string); isStr {
				f.values[i] = s
			}
		}
	}
	f.cur = len([]rune(f.values[0]))
	return f
}

// configArgs coerces a decoded TOML array into []string, mirroring the
// tolerance format.ExternalFromConfig applies to the same keys.
func configArgs(v any) []string {
	spec := format.ExternalFromConfig(map[string]any{"args": v})
	return spec.Args
}

// Title implements SubPanel (the breadcrumb segment).
func (f *formatForm) Title() string { return "Edit " + f.row.lang }

// Capturing implements SubPanel: every key is field text, so the form owns
// esc/enter itself.
func (f *formatForm) Capturing() bool { return true }

// Buttons implements SubPanel: click-only (the form captures keys); the key
// equivalents live in Update and the hint line.
func (f *formatForm) Buttons() []Button {
	return []Button{
		{Label: "Save", Do: f.save},
		{Label: "Reset to default", Do: f.reset},
		{Label: "Cancel", Do: func() tea.Cmd { f.host.Pop(); return nil }},
	}
}

// Update implements SubPanel.
func (f *formatForm) Update(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Code == tea.KeyEscape:
		f.host.Pop()
	case key.Code == tea.KeyEnter:
		return f.save()
	case key.Code == tea.KeyTab && key.Mod&tea.ModShift != 0, key.Code == tea.KeyUp:
		f.focus(f.field - 1)
	case key.Code == tea.KeyTab && f.fields[f.field].key == "command":
		// Tab completes the command path (#541 semantics); every other field
		// uses it to move on.
		f.values[f.field] = f.suggest.complete(f.values[f.field])
		f.cur = len([]rune(f.values[f.field]))
	case key.Code == tea.KeyTab, key.Code == tea.KeyDown:
		f.focus(f.field + 1)
	default:
		tf := newTextFieldAt(f.values[f.field], f.cur)
		if handled, changed := tf.Handle(key); handled {
			f.values[f.field], f.cur = tf.Text, tf.Cur
			if changed && f.fields[f.field].key == "command" {
				f.suggest.refresh(f.values[f.field])
			}
		}
	}
	return nil
}

// focus moves the field selection, wrapping, and re-seeds the cursor and the
// path suggestions.
func (f *formatForm) focus(idx int) {
	n := len(f.fields)
	f.field = ((idx % n) + n) % n
	f.cur = len([]rune(f.values[f.field]))
	if f.fields[f.field].key == "command" {
		f.suggest.refresh(f.values[f.field])
	} else {
		f.suggest.clear()
	}
}

// Click implements SubPanelClicker: a press on a field row focuses it, a
// press on a suggestion row takes it.
func (f *formatForm) Click(_, y int) tea.Cmd {
	if y >= 0 && y < len(f.fields) {
		f.focus(y)
		return nil
	}
	if idx := y - len(f.fields) - 1; idx >= 0 && idx < len(f.suggest.candidates) && idx < maxSuggestLines {
		f.values[f.field] = f.suggest.candidates[idx]
		f.cur = len([]rune(f.values[f.field]))
		f.suggest.refresh(f.values[f.field])
	}
	return nil
}

// View implements SubPanel: one row per field, the focused one carrying the
// cursor and its path suggestions, then the validation/hint line.
func (f *formatForm) View(w, h int) string {
	pal := f.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := make([]string, 0, h)
	for i, field := range f.fields {
		marker := "  "
		style := lipgloss.NewStyle()
		text := f.values[i]
		if i == f.field {
			marker = "▸ "
			style = style.Bold(true)
			text = newTextFieldAt(f.values[i], f.cur).View()
		}
		lines = append(lines, clip.Render(style.Render(" "+marker+pad(field.key, 12)+text)))
	}
	lines = append(lines, "")
	for _, s := range f.suggest.lines() {
		lines = append(lines, clip.Render(sec.Render(s)))
	}
	// The hints carry the focused field's accepted values and its default, so
	// they wrap rather than truncate (#553's footer rule inside the dialog).
	tail := []footerLine{{text: " " + f.fieldHint(), style: sec}}
	if f.note != "" {
		tail[0] = footerLine{text: " ✗ " + f.note, style: lipgloss.NewStyle().Foreground(pal.Error)}
	}
	tail = append(tail, footerLine{text: " enter saves into the " + f.page.scopeLabel() +
		" layer · a field left at the default is not written · esc cancels", style: sec})
	return strings.Join(append(lines, wrapFooter(tail, w, 4)...), "\n")
}

// fieldHint describes the focused field: its help text, the accepted values
// and the default it falls back to when emptied.
func (f *formatForm) fieldHint() string {
	field := f.fields[f.field]
	parts := []string{field.key}
	if field.help != "" {
		parts = append(parts, field.help)
	}
	switch field.kind {
	case fmtBool:
		parts = append(parts, "true | false")
	case fmtEnum:
		parts = append(parts, strings.Join(field.values, " | "))
	}
	if field.key == "command" {
		parts = append(parts, "tab completes")
	}
	if field.def != "" {
		parts = append(parts, "default: "+field.def)
	} else {
		parts = append(parts, "no default")
	}
	return strings.Join(parts, " · ")
}

// save validates the form and writes the whole table in one batch: keys that
// differ from the default are written, the rest removed.
func (f *formatForm) save() tea.Cmd {
	if msg := f.validate(); msg != "" {
		f.note = msg
		return nil
	}
	var muts []config.Mutation
	for i, field := range f.fields {
		key := "format." + f.row.lang + "." + field.key
		value := strings.TrimSpace(f.values[i])
		if value == "" || normalizeFormatValue(field, value) == normalizeFormatValue(field, field.def) {
			muts = append(muts, config.Mutation{Scope: f.page.scope, Key: key, Remove: true})
			continue
		}
		muts = append(muts, config.Mutation{Scope: f.page.scope, Key: key, Value: formatValue(field, value)})
	}
	f.host.Pop()
	return config.ApplyAndReload(f.page.opts, muts)
}

// reset drops the language's whole override (the page's "r" action) from the
// form's button row.
func (f *formatForm) reset() tea.Cmd {
	f.host.Pop()
	return f.page.reset(f.row)
}

// validate checks the typed values; "" means valid.
func (f *formatForm) validate() string {
	for i, field := range f.fields {
		value := strings.TrimSpace(f.values[i])
		if value == "" {
			continue
		}
		switch field.kind {
		case fmtBool:
			if value != "true" && value != "false" {
				return field.key + " must be true or false"
			}
		case fmtEnum:
			found := false
			for _, v := range field.values {
				if v == value {
					found = true
				}
			}
			if !found {
				return field.key + " must be one of " + strings.Join(field.values, ", ")
			}
		}
	}
	// range_args without a command is a spec that can never run: the range
	// arguments would be handed to nothing.
	if f.value("range_args") != "" && f.value("command") == "" && f.row.def.Command == "" {
		return "range_args needs a command"
	}
	return ""
}

// value returns the trimmed input of a field by key ("" when absent).
func (f *formatForm) value(key string) string {
	for i, field := range f.fields {
		if field.key == key {
			return strings.TrimSpace(f.values[i])
		}
	}
	return ""
}

// normalizeFormatValue renders a field's text in a comparable shape (argument
// lists collapse their whitespace).
func normalizeFormatValue(field formatField, value string) string {
	value = strings.TrimSpace(value)
	if field.kind == fmtList {
		return strings.Join(strings.Fields(value), " ")
	}
	return value
}

// formatValue converts a field's text into the TOML value written back.
func formatValue(field formatField, value string) any {
	switch field.kind {
	case fmtList:
		return strings.Fields(value)
	case fmtBool:
		return value == "true"
	default:
		return value
	}
}

func (f *formatForm) theme() *theme.Palette {
	if f.page.pal != nil {
		return f.page.pal
	}
	return theme.DefaultPalette()
}

// Paste inserts a pasted block into the focused field at its cursor (#2002);
// a paste into the command field refreshes the path suggestions exactly like
// typing there does.
func (f *formatForm) Paste(text string) bool {
	tf := newTextFieldAt(f.values[f.field], f.cur)
	if !tf.Paste(text) {
		return false
	}
	f.values[f.field], f.cur = tf.Text, tf.Cur
	if f.fields[f.field].key == "command" {
		f.suggest.refresh(f.values[f.field])
	}
	return true
}

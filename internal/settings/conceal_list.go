package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/concealfilter"
	"ike/internal/config"
	"ike/internal/numhint"
)

// conceal_list.go turns the four raw string lists behind the conceal suite —
// the two global glob lists, the per-family rules, the field→unit mapping and
// the secret key patterns — into structured editors (#2133). Each element has
// a grammar (`family=pattern`, `pattern=unit`, a `-` prefix meaning "exempt"),
// and typing that grammar into a comma-separated free-text field is exactly
// where the typos came from. Here the parts are separate fields, the element
// is composed from them, and the composed element is validated by the same
// code the config loader uses — concealfilter.Invalid for a rule,
// numhint.EntryError (through numberUnitValidate) for a mapping — so a bad
// entry is refused while it is typed rather than dropped with a diagnostic
// after the write.

// concealListPanel is the list editor for one config key, opened from the
// page's list row.
type concealListPanel struct {
	navRows
	page *ConcealPage
	host SubPanelHost
	row  concealRow

	entries []string
	sel     int
	off     int
	note    string

	listH int
}

// newConcealListPanel opens the editor over the key's live value.
func newConcealListPanel(page *ConcealPage, host SubPanelHost, row concealRow) *concealListPanel {
	return &concealListPanel{page: page, host: host, row: row,
		entries: append([]string(nil), concealList(row.key)...)}
}

func (p *concealListPanel) Title() string   { return p.row.title }
func (p *concealListPanel) Capturing() bool { return false }

func (p *concealListPanel) Buttons() []Button {
	return []Button{
		{Label: "Add", Key: "a", Do: func() tea.Cmd { p.openForm(-1); return nil }},
		{Label: "Edit", Do: func() tea.Cmd { p.openForm(p.sel); return nil }, Disabled: len(p.entries) == 0},
		{Label: "Delete", Key: "d", Do: p.deleteSelected, Disabled: len(p.entries) == 0},
	}
}

func (p *concealListPanel) Update(key tea.KeyPressMsg) tea.Cmd {
	if listNav(key.String(), &p.sel, len(p.entries), p.navPageSize()) {
		return nil
	}
	switch key.String() {
	case "enter":
		p.openForm(p.sel)
	case "K", "shift+up":
		return p.move(-1)
	case "J", "shift+down":
		return p.move(+1)
	}
	return nil
}

// openForm pushes the add (idx -1) or edit element form.
func (p *concealListPanel) openForm(idx int) {
	if idx >= len(p.entries) {
		return
	}
	p.note = ""
	p.host.Push(newConcealEntryForm(p, p.host, idx))
}

// deleteSelected drops the selected element.
func (p *concealListPanel) deleteSelected() tea.Cmd {
	if p.sel < 0 || p.sel >= len(p.entries) {
		return nil
	}
	next := append([]string(nil), p.entries...)
	next = append(next[:p.sel], next[p.sel+1:]...)
	if p.sel >= len(next) && p.sel > 0 {
		p.sel--
	}
	return p.commit(next)
}

// move reorders the selected element. Order is meaning in these lists: the
// first matching pattern decides a key in both pattern maps, so "move this
// exemption in front of the rule masking it" is an edit users need.
func (p *concealListPanel) move(delta int) tea.Cmd {
	to := p.sel + delta
	if p.sel < 0 || p.sel >= len(p.entries) || to < 0 || to >= len(p.entries) {
		return nil
	}
	next := append([]string(nil), p.entries...)
	next[p.sel], next[to] = next[to], next[p.sel]
	p.sel = to
	return p.commit(next)
}

// commit persists the whole list at user scope and reloads. The panel keeps
// its own copy: the reload is asynchronous, and a list editor that showed the
// pre-write value until the message came back would read as a lost edit.
func (p *concealListPanel) commit(next []string) tea.Cmd {
	p.entries = next
	return config.WriteAndReload(p.page.opts, config.UserScope, p.row.key, next)
}

// elementError reports why an element is invalid, "" when it is fine. It is
// the loader's own check, so the panel flags exactly what the loader drops.
func elementError(kind concealListKind, entry string) string {
	if strings.TrimSpace(entry) == "" {
		return "entry must not be empty"
	}
	switch kind {
	case concealRules:
		if bad := concealfilter.Invalid([]string{entry}); len(bad) > 0 {
			// Naming all seventeen families here would not fit the row; the
			// family field's own hints list them as they are typed.
			return "not a rule: write family=pattern, the family a registered one"
		}
	case concealUnits:
		return numberUnitValidate(nil, entry)
	}
	return ""
}

func (p *concealListPanel) View(w, h int) string {
	p.setRows(h)
	pal := p.page.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	list := make([]string, 0, len(p.entries))
	for i, e := range p.entries {
		line := " " + e
		if msg := elementError(p.row.list, e); msg != "" {
			line += "   ✗ " + msg
		}
		style := lipgloss.NewStyle().MaxWidth(w)
		if i == p.sel {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(line))
	}
	if len(p.entries) == 0 {
		list = append(list, sec.MaxWidth(w).Render(" no entries — press a to add one"))
	}
	var footer []footerLine
	if p.note != "" {
		footer = append(footer, footerLine{text: " " + p.note, style: sec})
	}
	footer = append(footer,
		footerLine{text: " " + concealListHint(p.row.list), style: sec},
		footerLine{text: " a add · enter edit · d delete · shift+↑/↓ reorder (first match wins) · esc closes", style: sec},
	)
	wrapped := wrapFooter(footer, w, 5)
	p.listH = h - len(wrapped)
	return pinFooter(list, wrapped, p.sel, p.sel, h, &p.off)
}

// concealListHint words the element grammar of one list kind.
func concealListHint(kind concealListKind) string {
	switch kind {
	case concealRules:
		return "family=pattern, the pattern prefixed - for an exclude: secret_masking=-**/testdata/**"
	case concealUnits:
		return "pattern=unit, the unit the field's raw numbers are read in: request_timeout=s"
	case concealSecretKeys:
		return "a key pattern to mask (*_LICENSE), or one prefixed - to exempt a key the built-ins mask"
	}
	return "a glob: *.py or Makefile matches the base name, vendor/** the whole path"
}

// Click focuses (and re-clicks open) an element row.
func (p *concealListPanel) Click(_, y int) tea.Cmd {
	if y < 0 || (p.listH > 0 && y >= p.listH) {
		return nil
	}
	idx := y + p.off
	if idx >= len(p.entries) {
		return nil
	}
	if idx == p.sel {
		p.openForm(idx)
		return nil
	}
	p.sel = idx
	return nil
}

// Wheel implements SubPanelWheeler.
func (p *concealListPanel) Wheel(delta int) {
	if n := len(p.entries); n > 0 {
		p.sel = clamp(p.sel+delta, 0, n-1)
	}
}

// --- element form ---

// concealField is one form field: a free-text part of the element, or a
// two-state choice (include/exclude, mask/exempt) cycled with ←/→.
type concealField struct {
	name string
	// opts non-empty makes the field a choice rather than free text.
	opts []string
	// hints lists candidate values under a free-text field as it is typed.
	hints func(text string) []string
}

// concealFields describes one list kind's element parts, in form order.
func concealFields(kind concealListKind) []concealField {
	switch kind {
	case concealRules:
		return []concealField{
			{name: "family", hints: func(t string) []string {
				return prefixed(concealfilter.Families(), strings.ToLower(strings.TrimSpace(t)))
			}},
			{name: "mode", opts: []string{"include", "exclude"}},
			{name: "glob", hints: func(string) []string { return []string{"*.log", "**/testdata/**", "vendor/**", ".env.example"} }},
		}
	case concealUnits:
		return []concealField{
			{name: "field pattern", hints: func(string) []string { return []string{"*_bytes", "retention", "created_at", "session_id"} }},
			{name: "unit", hints: func(t string) []string {
				if out := prefixed(numhint.UnitVocabulary(), strings.ToLower(strings.TrimSpace(t))); len(out) > 0 {
					return out
				}
				return numhint.UnitVocabulary()
			}},
		}
	case concealSecretKeys:
		return []concealField{
			{name: "mode", opts: []string{"mask", "exempt"}},
			{name: "key pattern", hints: func(string) []string { return []string{"MY_API_KEY", "*_LICENSE", "db_pass*"} }},
		}
	}
	return []concealField{
		{name: "glob", hints: func(string) []string { return []string{"*.py", "Makefile", "vendor/**", "**/config/**"} }},
	}
}

// concealParse splits an existing element into its form fields.
func concealParse(kind concealListKind, entry string) []string {
	switch kind {
	case concealRules:
		fam, pat, ok := strings.Cut(entry, "=")
		if !ok {
			return []string{strings.TrimSpace(entry), "include", ""}
		}
		fam = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fam)), "editor.")
		pat = strings.TrimSpace(pat)
		mode := "include"
		switch {
		case strings.HasPrefix(pat, "-"), strings.HasPrefix(pat, "!"):
			mode, pat = "exclude", pat[1:]
		case strings.HasPrefix(pat, "+"):
			pat = pat[1:]
		}
		return []string{fam, mode, strings.TrimSpace(pat)}
	case concealUnits:
		pat, unit, ok := strings.Cut(entry, "=")
		if !ok {
			return []string{strings.TrimSpace(entry), ""}
		}
		return []string{strings.TrimSpace(pat), strings.TrimSpace(unit)}
	case concealSecretKeys:
		pat := strings.TrimSpace(entry)
		if strings.HasPrefix(pat, "-") || strings.HasPrefix(pat, "!") {
			return []string{"exempt", strings.TrimSpace(pat[1:])}
		}
		return []string{"mask", pat}
	}
	return []string{strings.TrimSpace(entry)}
}

// concealCompose builds the element from its form fields.
func concealCompose(kind concealListKind, vals []string) string {
	trim := func(i int) string {
		if i < len(vals) {
			return strings.TrimSpace(vals[i])
		}
		return ""
	}
	switch kind {
	case concealRules:
		prefix := ""
		if trim(1) == "exclude" {
			prefix = "-"
		}
		return trim(0) + "=" + prefix + trim(2)
	case concealUnits:
		return trim(0) + "=" + trim(1)
	case concealSecretKeys:
		if trim(0) == "exempt" {
			return "-" + trim(1)
		}
		return trim(1)
	}
	return trim(0)
}

// concealEntryForm edits one element as separate fields.
type concealEntryForm struct {
	panel  *concealListPanel
	host   SubPanelHost
	idx    int
	fields []concealField
	vals   []string

	field int
	cur   int
	note  string
}

// newConcealEntryForm opens the form on element idx, or on a blank element
// when idx is negative.
func newConcealEntryForm(panel *concealListPanel, host SubPanelHost, idx int) *concealEntryForm {
	f := &concealEntryForm{panel: panel, host: host, idx: idx, fields: concealFields(panel.row.list)}
	if idx >= 0 && idx < len(panel.entries) {
		f.vals = concealParse(panel.row.list, panel.entries[idx])
	} else {
		f.vals = concealParse(panel.row.list, "")
	}
	for len(f.vals) < len(f.fields) {
		f.vals = append(f.vals, "")
	}
	f.cur = len([]rune(f.vals[0]))
	return f
}

func (f *concealEntryForm) Title() string {
	if f.idx < 0 {
		return "New Entry"
	}
	return "Edit Entry"
}

func (f *concealEntryForm) Capturing() bool { return true }

func (f *concealEntryForm) Buttons() []Button {
	return []Button{
		{Label: "Save", Do: f.save},
		{Label: "Cancel", Do: func() tea.Cmd { f.host.Pop(); return nil }},
	}
}

func (f *concealEntryForm) Update(key tea.KeyPressMsg) tea.Cmd {
	choice := len(f.fields[f.field].opts) > 0
	switch {
	case key.Code == tea.KeyEscape:
		f.host.Pop()
	case key.Code == tea.KeyEnter:
		return f.save()
	case key.Code == tea.KeyTab && key.Mod&tea.ModShift != 0, key.Code == tea.KeyUp:
		f.focus(f.field - 1)
	case key.Code == tea.KeyTab, key.Code == tea.KeyDown:
		f.focus(f.field + 1)
	case choice && (key.Code == tea.KeyLeft || key.Code == tea.KeyRight || key.Code == tea.KeySpace):
		f.cycle(key.Code == tea.KeyLeft)
	case choice:
		// A choice field takes no text; the letters that name its options
		// select them, so "e" picks exclude/exempt without arrowing.
		f.pick(key.String())
	default:
		tf := newTextFieldAt(f.vals[f.field], f.cur)
		if handled, _ := tf.Handle(key); handled {
			f.vals[f.field], f.cur = tf.Text, tf.Cur
		}
	}
	return nil
}

// focus moves to field i, wrapping.
func (f *concealEntryForm) focus(i int) {
	n := len(f.fields)
	f.field = ((i % n) + n) % n
	f.cur = len([]rune(f.vals[f.field]))
}

// cycle steps a choice field's value.
func (f *concealEntryForm) cycle(back bool) {
	opts := f.fields[f.field].opts
	at := 0
	for i, o := range opts {
		if o == f.vals[f.field] {
			at = i
		}
	}
	if back {
		at += len(opts) - 1
	} else {
		at++
	}
	f.vals[f.field] = opts[at%len(opts)]
}

// pick selects the choice option starting with the typed letter.
func (f *concealEntryForm) pick(s string) {
	for _, o := range f.fields[f.field].opts {
		if strings.HasPrefix(o, strings.ToLower(s)) {
			f.vals[f.field] = o
			return
		}
	}
}

// Click focuses a field row.
func (f *concealEntryForm) Click(_, y int) tea.Cmd {
	if y >= 0 && y < len(f.fields) {
		f.focus(y)
	}
	return nil
}

// save validates the composed element and writes the list back.
func (f *concealEntryForm) save() tea.Cmd {
	entry := concealCompose(f.panel.row.list, f.vals)
	if msg := elementError(f.panel.row.list, entry); msg != "" {
		f.note = msg
		return nil
	}
	next := append([]string(nil), f.panel.entries...)
	if f.idx >= 0 && f.idx < len(next) {
		next[f.idx] = entry
	} else {
		next = append(next, entry)
		f.panel.sel = len(next) - 1
	}
	f.host.Pop()
	return f.panel.commit(next)
}

func (f *concealEntryForm) View(w, h int) string {
	pal := f.panel.page.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := make([]string, 0, h)
	for i, fl := range f.fields {
		marker, style := "  ", lipgloss.NewStyle()
		text := f.vals[i]
		if len(fl.opts) > 0 {
			text = "‹ " + text + " ›"
		}
		if i == f.field {
			marker, style = "▸ ", style.Bold(true)
			if len(fl.opts) == 0 {
				text = newTextFieldAt(f.vals[i], f.cur).View()
			}
		}
		lines = append(lines, clip.Render(style.Render(" "+marker+pad(fl.name, 16)+text)))
	}
	lines = append(lines, "")
	lines = append(lines, clip.Render(sec.Render(" → "+concealCompose(f.panel.row.list, f.vals))))
	if f.note != "" {
		lines = append(lines, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+f.note)))
	} else if hints := f.hints(); len(hints) > 0 {
		lines = append(lines, clip.Render(sec.Render(" e.g. "+strings.Join(hints, ", "))))
	}
	lines = append(lines, clip.Render(sec.Render(" tab next field · enter saves · esc cancels")))
	return strings.Join(lines, "\n")
}

// hints lists the focused field's candidates, capped so the row stays one line.
func (f *concealEntryForm) hints() []string {
	fl := f.fields[f.field]
	if fl.hints == nil {
		return nil
	}
	out := fl.hints(f.vals[f.field])
	if len(out) > 6 {
		out = append(out[:6:6], "…")
	}
	return out
}

// Paste inserts a pasted block into the focused text field (#2002).
func (f *concealEntryForm) Paste(text string) bool {
	if len(f.fields[f.field].opts) > 0 {
		return false
	}
	tf := newTextFieldAt(f.vals[f.field], f.cur)
	if !tf.Paste(text) {
		return false
	}
	f.vals[f.field], f.cur = tf.Text, tf.Cur
	return true
}

package settings

import (
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/theme"
)

// associations_page.go is the [files.associations] editor (#1365): a custom
// settings page managing the user's file-pattern → language mappings from the
// UI. "a" adds, enter edits, "d" deletes; writes land at user scope — which
// files count as which language is a personal preference, not project state.

// AssocPage implements PageModel.
type AssocPage struct {
	navRows // last rendered height, the pgup/pgdn page (#1666)
	opts    config.Options
	pal     *theme.Palette

	sel  int
	off  int // list scroll offset
	note string
	host SubPanelHost

	listH int // list-window height of the last render (mouse hit-testing)
}

// SetSubPanelHost implements the hostAware injection seam (#883).
func (t *AssocPage) SetSubPanelHost(h SubPanelHost) { t.host = h }

// NewAssocPage builds the associations editor writing [files.associations]
// through opts.
func NewAssocPage(opts config.Options) *AssocPage {
	return &AssocPage{opts: opts}
}

// SetPalette implements PageModel.
func (t *AssocPage) SetPalette(p *theme.Palette) { t.pal = p }

// Capturing implements PageModel: the add/edit form is a sub-panel (#892), so
// the page never captures.
func (t *AssocPage) Capturing() bool { return false }

// association is one pattern → language row.
type association struct {
	Pattern string
	Lang    string
}

// entries returns the configured associations from the live config, sorted by
// pattern (the config holds a map; the list needs a stable order).
func (t *AssocPage) entries() []association {
	c := config.Get()
	if c == nil {
		return nil
	}
	out := make([]association, 0, len(c.Files.Associations))
	for p, id := range c.Files.Associations {
		out = append(out, association{Pattern: p, Lang: id})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pattern < out[j].Pattern })
	return out
}

// Update implements PageModel.
func (t *AssocPage) Update(key tea.KeyPressMsg) tea.Cmd {
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if listNav(key.String(), &t.sel, len(t.entries()), t.navPageSize()) {
		return nil
	}
	// Shared add·edit·delete actions (#2466).
	pageActionKey(key.String(), pageActions{
		host: t.host, pal: t.pal, sel: t.sel, n: len(t.entries()),
		open: t.openForm,
		confirm: func(idx int) string {
			e := t.entries()[idx]
			return "delete the association " + e.Pattern + " → " + e.Lang
		},
		remove: t.deleteEntry,
	})
	return nil
}

// openForm pushes the add (idx -1) or edit form sub-panel (#892).
func (t *AssocPage) openForm(idx int) {
	t.note = ""
	if t.host != nil {
		t.host.Push(newAssocForm(t, t.host, idx))
	}
}

// deleteEntry removes the entry at idx and writes the map back.
func (t *AssocPage) deleteEntry(idx int) tea.Cmd {
	entries := t.entries()
	if idx < 0 || idx >= len(entries) {
		return nil
	}
	if t.sel >= len(entries)-1 && t.sel > 0 {
		t.sel--
	}
	return writeAssociation(t.opts, entries[idx].Pattern, "", "")
}

// writeAssociation persists one association change at user scope and reloads:
// remove takes the old pattern's key out (rename or delete), then a non-empty
// pattern writes the new key. Both happen in one command so the single reload
// sees the final state.
func writeAssociation(opts config.Options, remove, pattern, id string) tea.Cmd {
	return func() tea.Msg {
		var diags []config.Diagnostic
		if remove != "" && remove != pattern {
			if err := config.RemoveKey(opts, config.UserScope, "files.associations."+remove); err != nil {
				diags = append(diags, config.Diagnostic{Field: "files.associations." + remove, Message: err.Error()})
			}
		}
		if pattern != "" {
			if err := config.WriteKey(opts, config.UserScope, "files.associations."+pattern, id); err != nil {
				diags = append(diags, config.Diagnostic{Field: "files.associations." + pattern, Message: err.Error()})
			}
		}
		c, loadDiags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: append(loadDiags, diags...)}
	}
}

// pageTheme returns the active palette, defaulting when none was threaded in.
func (t *AssocPage) theme() *theme.Palette {
	if t.pal != nil {
		return t.pal
	}
	return theme.DefaultPalette()
}

// View implements PageModel.
func (t *AssocPage) View(w, h int) string {
	t.setRows(h)
	pal := t.theme()
	head := " pattern → language   (extra extensions/filenames for registered languages, #1365)"
	entries := t.entries()
	var list []string
	for i, e := range entries {
		line := " " + pad(e.Pattern, 28) + "→ " + e.Lang
		if _, ok := lang.ByID(e.Lang); !ok {
			line += "   ✗ unknown language id"
		}
		style := lipgloss.NewStyle()
		if i == t.sel {
			style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		}
		list = append(list, style.Render(line))
	}
	if len(entries) == 0 {
		list = append(list, "no associations configured — press a to map a pattern like *.mytool to a language")
	}
	var footer []string
	{
		hint := "   a add · enter edit · d delete — patterns match the file's base name (*.ext or exact name)"
		lines := []footerLine{{text: hint, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}
		if t.note != "" {
			lines = append([]footerLine{{text: "   " + t.note, style: lipgloss.NewStyle().Foreground(pal.Secondary)}}, lines...)
		}
		footer = wrapFooter(lines, w, 3)
	}
	headLine := lipgloss.NewStyle().Foreground(pal.Secondary).Render(head)
	t.listH = h - 1 - len(footer)
	return headLine + "\n" + pinFooter(list, footer, t.sel, t.sel, h-1, &t.off)
}

// Click implements the optional PageClicker seam (enter semantics on the
// selected row).
func (t *AssocPage) Click(_, y int) tea.Cmd {
	return pageClick(y, t.off, t.listH, len(t.entries()), &t.sel, t.openForm)
}

// Wheel implements the optional PageWheeler seam.
func (t *AssocPage) Wheel(delta int) {
	if n := len(t.entries()); n > 0 {
		t.sel = clamp(t.sel+delta, 0, n-1)
	}
}

// KeyHelp implements KeyHelper (#887).
func (t *AssocPage) KeyHelp() []string {
	return []string{
		"a  add an association · enter  edit · d  delete",
		"pattern matches the base name: *.mytool, Jenkinsfile",
	}
}

// --- add/edit form ---

// assocFieldCount is the number of form fields: pattern, language.
const assocFieldCount = 2

var assocFieldNames = [assocFieldCount]string{"pattern", "language"}

// assocForm is the add/edit association form as a sub-panel (#892).
type assocForm struct {
	page *AssocPage
	host SubPanelHost
	idx  int

	fieldNav // focused field + cursor within it (#888, #2466)
	form     [assocFieldCount]string
	note     string
}

func newAssocForm(page *AssocPage, host SubPanelHost, idx int) *assocForm {
	f := &assocForm{page: page, host: host, idx: idx}
	f.fieldNav = newFieldNav(assocFieldCount, func(i int) string { return f.form[i] })
	if idx >= 0 {
		e := page.entries()[idx]
		f.form = [assocFieldCount]string{e.Pattern, e.Lang}
	}
	return f
}

func (f *assocForm) Title() string {
	if f.idx < 0 {
		return "New Association"
	}
	return "Edit Association"
}

func (f *assocForm) Capturing() bool { return true }

func (f *assocForm) Buttons() []Button {
	return []Button{
		{Label: "Save", Do: f.save},
		{Label: "Cancel", Do: func() tea.Cmd { f.host.Pop(); return nil }},
	}
}

func (f *assocForm) Update(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Code == tea.KeyEscape:
		f.host.Pop()
	case key.Code == tea.KeyEnter:
		return f.save()
	case f.fieldNav.Update(key): // shared field motion (#2466)
	default:
		tf := newTextFieldAt(f.form[f.field], f.cur)
		if handled, _ := tf.Handle(key); handled {
			f.form[f.field], f.cur = tf.text, tf.cur
		}
	}
	return nil
}

// Click focuses a field row.
func (f *assocForm) Click(_, y int) tea.Cmd {
	f.fieldNav.Focus(y)
	return nil
}

func (f *assocForm) validate() string {
	pattern := strings.TrimSpace(f.form[0])
	if pattern == "" {
		return "pattern is required (e.g. *.mytool or Jenkinsfile)"
	}
	if _, err := filepath.Match(pattern, "x"); err != nil {
		return "not a valid pattern: " + pattern
	}
	id := strings.TrimSpace(f.form[1])
	if id == "" {
		return "language id is required (e.g. toml, yaml, go)"
	}
	if _, ok := lang.ByID(id); !ok {
		return "unknown language id " + id + " — see the registered languages"
	}
	for i, e := range f.page.entries() {
		if i != f.idx && e.Pattern == pattern {
			return "an association for " + pattern + " already exists"
		}
	}
	return ""
}

func (f *assocForm) save() tea.Cmd {
	if msg := f.validate(); msg != "" {
		f.note = msg
		return nil
	}
	var remove string
	if entries := f.page.entries(); f.idx >= 0 && f.idx < len(entries) {
		remove = entries[f.idx].Pattern
	}
	f.host.Pop()
	return writeAssociation(f.page.opts, remove, strings.TrimSpace(f.form[0]), strings.TrimSpace(f.form[1]))
}

func (f *assocForm) View(w, h int) string {
	pal := f.page.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	lines := make([]string, 0, h)
	for i, name := range assocFieldNames {
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
		ids := make([]string, 0, 8)
		for _, l := range lang.All() {
			ids = append(ids, l.ID)
			if len(ids) == 8 {
				break
			}
		}
		lines = append(lines, clip.Render(sec.Render(" tab next field · enter saves · esc cancels")))
		lines = append(lines, clip.Render(sec.Render(" language ids: "+strings.Join(ids, ", ")+", …")))
	}
	return strings.Join(lines, "\n")
}

// Paste inserts a pasted block into the focused field at its cursor (#2002),
// through the same shared helper the typed keys use.
func (f *assocForm) Paste(text string) bool {
	tf := newTextFieldAt(f.form[f.field], f.cur)
	if !tf.Paste(text) {
		return false
	}
	f.form[f.field], f.cur = tf.text, tf.cur
	return true
}

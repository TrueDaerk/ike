package settings

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

// projectgroups_form.go is the project-group add/edit form as a SubPanel
// (#883, Epic 0510 §6): one ui.Field for the name and one per member root, so
// the roots are a growing list of rows instead of a comma-joined line — a
// group's roots are paths, and paths carry commas.
//
// Row keys: "+" on an empty root row (and alt+enter anywhere) appends a row,
// "-" / alt+backspace on an empty root row removes it. The empty-row rule is
// what keeps "+" and "-" typable inside a path — a directory may well be
// named "c++" — while still putting the two verbs on the plain keys.

// groupForm implements SubPanel.
type groupForm struct {
	page *ProjectGroupsPage
	host SubPanelHost
	idx  int // group being edited, -1 for a new one

	fieldNav // focused field + cursor within it (#888, #2466)
	name     string
	roots    []string // one field per root, always at least one row
	note     string
}

// newGroupForm seeds the form from the group at idx (-1 = blank).
func newGroupForm(page *ProjectGroupsPage, host SubPanelHost, idx int) *groupForm {
	f := &groupForm{page: page, host: host, idx: idx, roots: []string{""}}
	if idx >= 0 && idx < len(page.entries()) {
		g := page.entries()[idx]
		f.name = g.Name
		if len(g.Roots) > 0 {
			f.roots = append([]string(nil), g.Roots...)
		}
	}
	f.syncNav()
	f.Focus(0) // the caret parks at the end of the seeded name, ready to edit
	return f
}

// fieldCount is the number of rows: the name plus one per root.
func (f *groupForm) fieldCount() int { return 1 + len(f.roots) }

// fieldText reports row i's text (0 = name, 1+ = the roots).
func (f *groupForm) fieldText(i int) string {
	if i <= 0 {
		return f.name
	}
	if i-1 < len(f.roots) {
		return f.roots[i-1]
	}
	return ""
}

// setFieldText writes row i's text back.
func (f *groupForm) setFieldText(i int, s string) {
	if i <= 0 {
		f.name = s
		return
	}
	if i-1 < len(f.roots) {
		f.roots[i-1] = s
	}
}

// syncNav rebuilds the field cursor after the row count changed, keeping the
// focus in range and the caret inside its field.
func (f *groupForm) syncNav() {
	focus, cur := f.field, f.cur
	f.fieldNav = newFieldNav(f.fieldCount(), f.fieldText)
	if focus >= f.fieldCount() {
		focus = f.fieldCount() - 1
	}
	if focus < 0 {
		focus = 0
	}
	f.field = focus
	if n := len([]rune(f.fieldText(focus))); cur > n {
		cur = n
	}
	f.cur = cur
}

// Title implements SubPanel (the breadcrumb segment).
func (f *groupForm) Title() string {
	if f.idx < 0 {
		return "New Project Group"
	}
	return "Edit Project Group"
}

// Capturing implements SubPanel: every key is field text (a path may contain
// anything), so the form owns esc/enter itself.
func (f *groupForm) Capturing() bool { return true }

// Buttons implements SubPanel: click-only here (the form captures keys); the
// key equivalents are handled in Update and shown in the hint line.
func (f *groupForm) Buttons() []Button {
	return []Button{
		{Label: "Save", Do: f.save},
		{Label: "Add root", Do: func() tea.Cmd { f.addRoot(); return nil }},
		{Label: "Cancel", Do: func() tea.Cmd { f.host.Pop(); return nil }},
	}
}

// onRootRow reports whether the focus sits on a root row, and which one.
func (f *groupForm) onRootRow() (int, bool) {
	if f.field >= 1 && f.field-1 < len(f.roots) {
		return f.field - 1, true
	}
	return 0, false
}

// addRoot appends an empty root row after the focused one and focuses it.
func (f *groupForm) addRoot() {
	at := len(f.roots)
	if i, ok := f.onRootRow(); ok {
		at = i + 1
	}
	f.roots = append(f.roots, "")
	copy(f.roots[at+1:], f.roots[at:])
	f.roots[at] = ""
	f.syncNav()
	f.Focus(at + 1)
}

// removeRoot drops root row i, keeping at least one (empty) row so the form
// never renders without a root field.
func (f *groupForm) removeRoot(i int) {
	if i < 0 || i >= len(f.roots) {
		return
	}
	if len(f.roots) == 1 {
		f.roots[0] = ""
		f.syncNav()
		return
	}
	f.roots = append(f.roots[:i], f.roots[i+1:]...)
	f.syncNav()
	f.Focus(1 + clamp(i, 0, len(f.roots)-1))
}

// Update implements SubPanel.
func (f *groupForm) Update(key tea.KeyPressMsg) tea.Cmd {
	row, onRoot := f.onRootRow()
	emptyRow := onRoot && strings.TrimSpace(f.roots[row]) == ""
	switch {
	case key.Code == tea.KeyEscape:
		f.host.Pop()
	case key.Code == tea.KeyEnter && key.Mod&tea.ModAlt != 0:
		f.addRoot()
	case key.Code == tea.KeyEnter:
		return f.save()
	case key.Code == tea.KeyBackspace && key.Mod&tea.ModAlt != 0 && emptyRow:
		f.removeRoot(row)
	case key.String() == "+" && emptyRow:
		f.addRoot()
	case key.String() == "-" && emptyRow:
		f.removeRoot(row)
	case f.fieldNav.Update(key): // shared field motion (#2466)
	default:
		// Shared cursor input (#888).
		tf := newTextFieldAt(f.fieldText(f.field), f.cur)
		if handled, _ := tf.Handle(key); handled {
			f.setFieldText(f.field, tf.Text)
			f.cur = tf.Cur
		}
	}
	return nil
}

// Click implements SubPanelClicker: a press on a field row focuses it.
func (f *groupForm) Click(_, y int) tea.Cmd {
	if y == 0 {
		f.Focus(0)
		return nil
	}
	// Row 1 is the "roots" caption, the root fields start below it.
	if r := y - 2; r >= 0 && r < len(f.roots) {
		f.Focus(1 + r)
	}
	return nil
}

// Paste inserts a pasted block into the focused field at its cursor (#2002),
// through the same shared helper the typed keys use — a root is usually
// pasted, not typed.
func (f *groupForm) Paste(text string) bool {
	tf := newTextFieldAt(f.fieldText(f.field), f.cur)
	if !tf.Paste(text) {
		return false
	}
	f.setFieldText(f.field, tf.Text)
	f.cur = tf.Cur
	return true
}

// View implements SubPanel: the name row, the numbered root rows, then the
// validation/hint line.
func (f *groupForm) View(w, h int) string {
	pal := f.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	clip := lipgloss.NewStyle().MaxWidth(w)
	row := func(i int, label, text string) string {
		marker, style := "  ", lipgloss.NewStyle()
		if i == f.field {
			marker, style = "▸ ", style.Bold(true)
			text = newTextFieldAt(f.fieldText(i), f.cur).View()
		}
		return clip.Render(style.Render(" " + marker + pad(label, 10) + text))
	}
	lines := []string{row(0, "name", f.name)}
	lines = append(lines, clip.Render(sec.Render("   roots")))
	for i := range f.roots {
		lines = append(lines, row(1+i, "  "+strconv.Itoa(i+1)+".", f.roots[i]))
	}
	lines = append(lines, "")
	if f.note != "" {
		lines = append(lines, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+f.note)))
	} else {
		lines = append(lines, clip.Render(sec.Render(" a relative root resolves against the project directory · ~ expands")))
	}
	lines = append(lines, clip.Render(sec.Render(" tab next field · + add a root · - remove an empty root · enter saves · esc cancels")))
	return strings.Join(lines, "\n")
}

// theme returns the active palette, defaulting when none was threaded in.
func (f *groupForm) theme() *theme.Palette {
	if f.page != nil && f.page.pal != nil {
		return f.page.pal
	}
	return theme.DefaultPalette()
}

// validate checks the form and returns the message to show; "" means valid.
// It reports the first failure only, but names it precisely — which root row
// is wrong, and which group already owns the name.
func (f *groupForm) validate() (config.ProjectGroup, string) {
	ops := f.page.ops
	if ops.ResolveRoot == nil || ops.Validate == nil {
		return config.ProjectGroup{}, "editing groups is not available in this build"
	}
	name := strings.TrimSpace(f.name)
	if name == "" {
		return config.ProjectGroup{}, "name is required"
	}
	for i, g := range f.page.entries() {
		if i != f.idx && strings.EqualFold(g.Name, name) {
			return config.ProjectGroup{}, fmt.Sprintf("name already used by %q", g.Name)
		}
	}
	typed := trimAll(f.roots)
	if len(typed) == 0 {
		return config.ProjectGroup{}, "add at least one project root"
	}
	roots := make([]string, 0, len(typed))
	for i, r := range typed {
		// A relative root resolves against the project directory, a leading
		// "~" expands, and the result must be a readable directory.
		abs, err := ops.ResolveRoot(r)
		if err != nil {
			return config.ProjectGroup{}, fmt.Sprintf("root %d: %v", i+1, err)
		}
		roots = append(roots, abs)
	}
	// The data layer stays the gate (#2570): it owns the remaining rules —
	// path separators in the name, case-insensitive collisions, dedupe.
	validName, validRoots, err := ops.Validate(name, roots)
	if err != nil {
		return config.ProjectGroup{}, err.Error()
	}
	g := config.ProjectGroup{Name: validName, Roots: validRoots}
	if f.idx >= 0 && f.idx < len(f.page.entries()) {
		g.Created = f.page.entries()[f.idx].Created // an edit keeps its birthday
	}
	return g, ""
}

// save validates and writes the group; success pops back to the list.
func (f *groupForm) save() tea.Cmd {
	g, msg := f.validate()
	if msg != "" {
		f.note = msg
		return nil
	}
	groups := append([]config.ProjectGroup(nil), f.page.entries()...)
	if f.idx >= 0 && f.idx < len(groups) {
		groups[f.idx] = g // a rename keeps the group's list position
	} else {
		groups = append(groups, g)
	}
	f.host.Pop()
	return f.page.writeEntries(groups)
}

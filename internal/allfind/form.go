// Package allfind is the all-projects text search (#2394): a query form over
// every root in the recent-projects history, a background multi-root scan
// (internal/search.MultiService), and — once the scan is through — the
// find-in-path results overlay, grouped project → file → matches (#2413). The
// form mirrors the find-in-path overlay (internal/finder) — same toggles, glob
// fields and single-line editing — but confirming closes it immediately and
// hands the scan to the root model; while it runs only a status-line segment
// counts the projects, and the results open when it finishes.
package allfind

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
	"ike/internal/ui"
)

// State is the form's persisted content (#2394): the last query, toggles,
// globs and the excluded project roots, stored in the user config layer so
// the next open — in any project — resumes it.
type State struct {
	Query         string
	Include       string // comma-separated include globs
	Exclude       string // comma-separated exclude globs
	CaseSensitive bool
	WholeWord     bool
	Regex         bool
	ExcludedRoots []string
}

// Project is one history entry offered by the form's project list. Missing
// marks a root that no longer exists on disk: rendered greyed out, never
// scanned, and its exclusion toggle is inert.
type Project struct {
	Root     string
	Name     string
	Missing  bool
	Excluded bool
}

// ConfirmMsg asks the root model to start the background scan: persist the
// state and fan the query out over the kept roots. Roots lists the projects
// to scan, in history order (excluded and missing entries already dropped).
type ConfirmMsg struct {
	State State
	Roots []Project
}

// field enumerates the focusable form sections; tab cycles through them.
type field int

const (
	fieldQuery field = iota
	fieldInclude
	fieldExclude
	fieldProjects
	fieldCount
)

// Form is the query-form overlay state. The root model routes keys here while
// open; enter dispatches ConfirmMsg and closes.
type Form struct {
	open  bool
	focus field
	cur   int // rune cursor within the focused text field (#763)

	query   string
	include string
	exclude string

	caseSensitive bool
	wholeWord     bool
	regex         bool

	// preselect marks the remembered query as selected on open (#277): the
	// first typed character replaces it wholesale.
	preselect bool

	projects []Project
	projCur  int // highlighted row while fieldProjects has the focus

	// lay maps content rows to click targets; View fills it each render.
	lay formLayout

	width, height int
	pal           *theme.Palette
}

// NewForm returns a closed form.
func NewForm() *Form { return &Form{} }

// SetPalette threads the active theme in.
func (f *Form) SetPalette(p *theme.Palette) { f.pal = p }

// SetSize records the terminal size.
func (f *Form) SetSize(w, h int) { f.width, f.height = w, h }

// Open shows the form seeded from the persisted state and the project list
// built from the recent-projects history. sel is the active editor selection:
// non-empty (single-line) it prefills the query, selected, outranking the
// remembered one; otherwise the remembered query arrives preselected.
func (f *Form) Open(st State, projects []Project, sel string) {
	f.query = st.Query
	f.include = st.Include
	f.exclude = st.Exclude
	f.caseSensitive = st.CaseSensitive
	f.wholeWord = st.WholeWord
	f.regex = st.Regex
	if q, ok := prefillQuery(sel, f.regex); ok {
		f.query = q
	}
	f.projects = projects
	f.projCur = 0
	f.open = true
	f.focus = fieldQuery
	f.cur = len([]rune(f.query))
	f.preselect = f.query != ""
}

// prefillQuery turns a selection into a query prefill, on the finder's terms
// (#2165): blank or line-spanning selections prefill nothing; regex mode
// escapes the text so it matches literally.
func prefillQuery(sel string, regex bool) (string, bool) {
	line := strings.TrimSuffix(strings.TrimSuffix(sel, "\n"), "\r")
	if strings.TrimSpace(line) == "" || strings.ContainsAny(line, "\n\r") {
		return "", false
	}
	if regex {
		line = regexp.QuoteMeta(line)
	}
	return line, true
}

// Close hides the form without confirming.
func (f *Form) Close() { f.open = false }

// IsOpen reports whether the form is shown.
func (f *Form) IsOpen() bool { return f.open }

// State returns the form's current content in its persistable shape.
func (f *Form) State() State {
	var excluded []string
	for _, p := range f.projects {
		if p.Excluded {
			excluded = append(excluded, p.Root)
		}
	}
	return State{
		Query:         f.query,
		Include:       f.include,
		Exclude:       f.exclude,
		CaseSensitive: f.caseSensitive,
		WholeWord:     f.wholeWord,
		Regex:         f.regex,
		ExcludedRoots: excluded,
	}
}

// keptRoots lists the projects the scan covers: history order, minus the
// excluded and the missing.
func (f *Form) keptRoots() []Project {
	var out []Project
	for _, p := range f.projects {
		if !p.Excluded && !p.Missing {
			out = append(out, p)
		}
	}
	return out
}

// confirm dispatches ConfirmMsg and closes. An empty query, or a project list
// with nothing left to scan, keeps the form open — there is nothing to run.
func (f *Form) confirm() tea.Cmd {
	if strings.TrimSpace(f.query) == "" {
		return nil
	}
	roots := f.keptRoots()
	if len(roots) == 0 {
		return nil
	}
	msg := ConfirmMsg{State: f.State(), Roots: roots}
	f.Close()
	return func() tea.Msg { return msg }
}

// Paste inserts a pasted block into the focused text field (#1273). A pending
// prefill selection is replaced wholesale, like a typed character.
func (f *Form) Paste(text string) (handled bool) {
	if f.focus == fieldProjects {
		return false
	}
	pre := f.preselect
	fld := f.focused()
	cur := f.cur
	if pre && f.focus == fieldQuery {
		*fld, cur = "", 0
	}
	out, ncur, changed := ui.PasteText(*fld, cur, text)
	if !changed {
		return false
	}
	f.preselect = false
	*fld, f.cur = out, ncur
	return true
}

// Update handles one key while the form is open.
func (f *Form) Update(msg tea.KeyPressMsg) tea.Cmd {
	pre := f.preselect
	f.preselect = false
	switch msg.String() {
	case "esc":
		f.Close()
		return nil
	case "enter":
		if f.focus == fieldProjects {
			f.toggleProject(f.projCur)
			return nil
		}
		return f.confirm()
	case "tab":
		f.setFocus((f.focus + 1) % fieldCount)
		return nil
	case "shift+tab":
		f.setFocus((f.focus + fieldCount - 1) % fieldCount)
		return nil
	// ctrl doubles alt like the finder (#422): on macOS Option composes and
	// alt chords never reach the terminal.
	case "alt+c", "ctrl+c":
		f.caseSensitive = !f.caseSensitive
		return nil
	case "alt+w", "ctrl+w":
		f.wholeWord = !f.wholeWord
		return nil
	case "alt+x", "ctrl+x":
		f.regex = !f.regex
		return nil
	case "up":
		if f.focus == fieldProjects && f.projCur > 0 {
			f.projCur--
		}
		return nil
	case "down":
		if f.focus == fieldProjects && f.projCur < len(f.projects)-1 {
			f.projCur++
		}
		return nil
	case "space":
		if f.focus == fieldProjects {
			f.toggleProject(f.projCur)
			return nil
		}
	}
	if f.focus == fieldProjects {
		return nil
	}
	// Everything else is single-line editing on the focused field (#763).
	fld := f.focused()
	if out, ncur, handled, changed := ui.EditKey(msg, *fld, f.cur); handled {
		if pre && f.focus == fieldQuery && len(out) > len(*fld) {
			// The key inserted text: it replaces the selected prefill (#277).
			out, ncur, _, _ = ui.EditKey(msg, "", 0)
		}
		*fld, f.cur = out, ncur
		_ = changed
	}
	return nil
}

// toggleProject flips the exclusion of the project at idx; a missing root's
// toggle is inert — it is skipped either way.
func (f *Form) toggleProject(idx int) {
	if idx < 0 || idx >= len(f.projects) || f.projects[idx].Missing {
		return
	}
	f.projects[idx].Excluded = !f.projects[idx].Excluded
}

// setFocus moves the input focus, parking the text cursor at the field's end.
func (f *Form) setFocus(fld field) {
	f.focus = fld
	if fld != fieldProjects {
		f.cur = len([]rune(*f.focused()))
	}
}

// focused returns the text field the cursor edits (never fieldProjects).
func (f *Form) focused() *string {
	switch f.focus {
	case fieldInclude:
		return &f.include
	case fieldExclude:
		return &f.exclude
	}
	return &f.query
}

// formLayout maps content rows (0 = first row inside the border) to click
// targets; -1 marks an absent row.
type formLayout struct {
	query, toggles, include, exclude int
	projTop, projRows                int
}

// maxFormWidth caps the centered form box.
const maxFormWidth = 100

// boxWidth is the form's outer width, terminal minus a margin, capped.
func (f *Form) boxWidth() int {
	w := f.width - 12
	if w > maxFormWidth {
		w = maxFormWidth
	}
	if w < 40 {
		w = min(40, f.width-2)
	}
	return w
}

// maxProjectRows bounds the project list; longer histories scroll around the
// highlighted row.
const maxProjectRows = 8

// Click handles a left press at panel-local coordinates (0,0 = the box's
// top-left border cell): an input row takes focus, a toggle flips, a project
// row flips its exclusion.
func (f *Form) Click(x, y int) tea.Cmd {
	if !f.open || f.lay.query <= 0 {
		return nil
	}
	cx, cy := x-2, y-1 // border + horizontal padding
	if cx < 0 || cy < 0 {
		return nil
	}
	switch cy {
	case f.lay.query:
		f.setFocus(fieldQuery)
		f.preselect = false
	case f.lay.include:
		f.setFocus(fieldInclude)
	case f.lay.exclude:
		f.setFocus(fieldExclude)
	case f.lay.toggles:
		for i, sp := range toggleSpans() {
			if cx < sp[0] || cx >= sp[1] {
				continue
			}
			switch i {
			case 0:
				f.caseSensitive = !f.caseSensitive
			case 1:
				f.wholeWord = !f.wholeWord
			case 2:
				f.regex = !f.regex
			}
			break
		}
	}
	if f.lay.projTop >= 0 && cy >= f.lay.projTop && cy < f.lay.projTop+f.lay.projRows {
		idx := f.projScrollTop() + cy - f.lay.projTop
		if idx >= 0 && idx < len(f.projects) {
			f.setFocus(fieldProjects)
			f.projCur = idx
			f.toggleProject(idx)
		}
	}
	return nil
}

// projScrollTop is the first visible project row, keeping the highlight in
// the window.
func (f *Form) projScrollTop() int {
	if len(f.projects) <= maxProjectRows {
		return 0
	}
	top := f.projCur - maxProjectRows/2
	if top < 0 {
		top = 0
	}
	if top > len(f.projects)-maxProjectRows {
		top = len(f.projects) - maxProjectRows
	}
	return top
}

// View renders the centered form box.
func (f *Form) View() string {
	if !f.open || f.width <= 0 {
		return ""
	}
	pal := f.theme()
	boxW := f.boxWidth()
	innerW := boxW - 6 // border + padding (#971)

	title := lipgloss.NewStyle().Bold(true).Underline(true).Render("Find in All Projects")
	lay := formLayout{projTop: -1}
	rows := []string{title, ""}
	lay.query = len(rows)
	rows = append(rows, f.inputRow("Search ", f.query, fieldQuery, innerW))
	lay.toggles = len(rows)
	rows = append(rows, f.togglesRow(innerW))
	lay.include = len(rows)
	rows = append(rows, f.inputRow("Include", f.include, fieldInclude, innerW))
	lay.exclude = len(rows)
	rows = append(rows, f.inputRow("Exclude", f.exclude, fieldExclude, innerW))
	rows = append(rows, "")
	rows = append(rows, f.projectsHeading(innerW))
	lay.projTop = len(rows)
	proj := f.projectRows(innerW)
	lay.projRows = len(proj)
	rows = append(rows, proj...)
	rows = append(rows, "", f.statusRow(innerW))
	f.lay = lay

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1).
		Width(boxW - 2)
	return box.Render(strings.Join(rows, "\n"))
}

// theme returns the active palette, defaulting when none was threaded in.
func (f *Form) theme() *theme.Palette {
	if f.pal != nil {
		return f.pal
	}
	return theme.DefaultPalette()
}

// inputRow renders one labelled input line with a block cursor on the focused
// field, on the finder's terms.
func (f *Form) inputRow(label, value string, fld field, width int) string {
	pal := f.theme()
	lab := lipgloss.NewStyle().Faint(true).Render(label + " ")
	text := value
	switch {
	case fld == fieldQuery && f.preselect && value != "":
		text = lipgloss.NewStyle().Reverse(true).Render(value)
		if f.focus == fld {
			text += lipgloss.NewStyle().Reverse(true).Render(" ")
		}
	case f.focus == fld:
		text = ui.CursorView(value, f.cur)
	}
	row := lab + text
	if f.focus == fld {
		row = lipgloss.NewStyle().Foreground(pal.Foreground).Render(lab) + text
	}
	return ansi.Truncate(row, width, "…")
}

// The toggle row's fixed pieces; toggleSpans derives the click ranges.
const (
	togglesIndent = "        "
	caseLabel     = "Case (ctrl+c)"
	wordLabel     = "Word (ctrl+w)"
	regexLabel    = "Regex (ctrl+x)"
)

// toggleSpans mirrors togglesRow's layout: the half-open x range of each
// toggle within the content row.
func toggleSpans() [3][2]int {
	labels := [3]string{caseLabel, wordLabel, regexLabel}
	var spans [3][2]int
	x := len(togglesIndent)
	for i, l := range labels {
		w := 4 + len(l)
		spans[i] = [2]int{x, x + w}
		x += w + 2
	}
	return spans
}

// togglesRow renders the three match-mode toggles with their key hints.
func (f *Form) togglesRow(width int) string {
	pal := f.theme()
	on := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true)
	off := lipgloss.NewStyle().Faint(true)
	part := func(label string, active bool) string {
		if active {
			return on.Render("[x] " + label)
		}
		return off.Render("[ ] " + label)
	}
	row := togglesIndent + part(caseLabel, f.caseSensitive) +
		"  " + part(wordLabel, f.wholeWord) +
		"  " + part(regexLabel, f.regex)
	return ansi.Truncate(row, width, "…")
}

// projectsHeading labels the project list with the kept/total count.
func (f *Form) projectsHeading(width int) string {
	kept := len(f.keptRoots())
	head := "Projects (" + itoa(kept) + " of " + itoa(len(f.projects)) + " searched — space toggles)"
	style := lipgloss.NewStyle().Faint(true)
	if f.focus == fieldProjects {
		style = lipgloss.NewStyle().Foreground(f.theme().Foreground)
	}
	return ansi.Truncate(style.Render(head), width, "…")
}

// projectRows renders the visible window of the toggleable project list:
// checkbox, name, dimmed root; missing roots grey out with a "(missing)" tag.
func (f *Form) projectRows(width int) []string {
	pal := f.theme()
	dim := lipgloss.NewStyle().Faint(true)
	var out []string
	top := f.projScrollTop()
	end := min(top+maxProjectRows, len(f.projects))
	for i := top; i < end; i++ {
		p := f.projects[i]
		mark := "[x] "
		if p.Excluded || p.Missing {
			mark = "[ ] "
		}
		line := mark + p.Name + "  " + p.Root
		switch {
		case p.Missing:
			line = dim.Render(mark + p.Name + "  " + p.Root + "  (missing)")
		case f.focus == fieldProjects && i == f.projCur:
			line = lipgloss.NewStyle().Reverse(true).Render(ansi.Truncate(line, width-2, "…"))
		case p.Excluded:
			line = dim.Render(line)
		default:
			line = lipgloss.NewStyle().Foreground(pal.Foreground).Render(mark+p.Name) + dim.Render("  "+p.Root)
		}
		out = append(out, "  "+ansi.Truncate(line, width-2, "…"))
	}
	if len(out) == 0 {
		out = append(out, dim.Render("  no recent projects"))
	}
	return out
}

// statusRow spells out the form's keys.
func (f *Form) statusRow(width int) string {
	dim := lipgloss.NewStyle().Faint(true)
	return dim.Render(ansi.Truncate(
		"enter searches in the background — tab cycles fields, space toggles a project, esc closes",
		width, "…"))
}

// itoa is strconv.Itoa without the import weight elsewhere in the file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

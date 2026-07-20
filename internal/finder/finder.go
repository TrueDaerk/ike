// Package finder is the find-in-path overlay (Roadmap 0150, #85): a centered
// modal with a query input, case/word/regex toggles, include/exclude glob
// fields, query history, and a live-streamed results list (the reusable
// locations component). It drives internal/search directly — each edit starts
// a new scan (the service cancels the previous one) — and consumes the
// generation-tagged result messages the root model routes back in. Selecting
// a match dispatches OpenLocationMsg; the results survive closing, so
// next/prev-match commands keep working without the overlay.
package finder

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/locations"
	"ike/internal/search"
	"ike/internal/theme"
	"ike/internal/ui"
)

// OpenLocationMsg asks the root model to open Path at the (1-based) Line and
// (0-based rune) Col of a selected match.
type OpenLocationMsg struct {
	Path string
	Line int
	Col  int
}

// ReplaceRequestMsg asks the root model to apply Replacement to the given
// matches (Roadmap 0150, #86): through the open dirty buffer where one holds
// the file, directly on disk otherwise. Query carries the flags capture-group
// expansion needs.
type ReplaceRequestMsg struct {
	Items       []locations.Item
	Replacement string
	Query       search.Query
}

// field enumerates the focusable inputs; tab cycles through them (the replace
// field only exists in replace mode).
type field int

const (
	fieldQuery field = iota
	fieldReplace
	fieldInclude
	fieldExclude
	fieldCount
)

const maxHistory = 50

// Model is the overlay state. The root model routes keys here while open and
// feeds search.BatchMsg/DoneMsg through Apply.
type Model struct {
	svc  *search.Service
	root string

	open  bool
	focus field
	// cur is the rune cursor within the focused field (#763); focus changes
	// and programmatic field replacement reset it to the end.
	cur int

	query   string
	replace string // replacement template (replace mode only)
	include string // comma-separated include globs
	exclude string // comma-separated exclude globs

	// preselect marks the remembered query as selected on re-open (#277):
	// the first typed character replaces it wholesale, any other key keeps
	// the text and drops the mark — JetBrains' prefill-selected behavior.
	preselect bool

	// replaceMode adds the replacement input, the before/after preview, and
	// the apply keys (project.replaceInPath); off is plain find-in-path.
	replaceMode bool
	// lastQuery is the search.Query of the current scan, retained so apply
	// requests carry the exact flags the matches were produced with.
	lastQuery search.Query

	caseSensitive bool
	wholeWord     bool
	regex         bool

	hist    []string
	histIdx int // -1 = editing live, otherwise index into hist (newest first)

	gen       int // generation of the scan whose results we accept
	scanning  bool
	truncated bool
	errText   string

	list locations.List

	// lay records, during View, which content rows the mouse can hit; Click
	// hit-tests against it.
	lay layoutInfo

	width, height int
	pal           *theme.Palette

	// displayPath shortens result paths for the group headers (the root model
	// injects its project-relative formatter).
	displayPath func(string) string
}

// New returns a closed finder driving svc.
func New(svc *search.Service) *Model {
	return &Model{svc: svc, histIdx: -1}
}

// SetPalette threads the active theme in.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetDisplayPath injects the header path formatter.
func (m *Model) SetDisplayPath(f func(string) string) { m.displayPath = f }

// SetSize records the terminal size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// Open shows the finder rooted at root, keeping the previous query and
// toggles (JetBrains re-opens with the last search) but clearing results.
func (m *Model) Open(root string) {
	m.open = true
	m.replaceMode = false
	m.root = root
	m.focus = fieldQuery
	m.cur = len([]rune(m.query))
	m.histIdx = -1
	m.errText = ""
	m.preselect = m.query != ""
	m.rescan()
}

// OpenReplace shows the finder in replace mode (#86): find-in-path plus the
// replacement input, preview, and apply keys.
func (m *Model) OpenReplace(root string) {
	m.Open(root)
	m.replaceMode = true
}

// Close hides the overlay. Results are kept for next/prev-match.
func (m *Model) Close() {
	m.open = false
	m.svc.Cancel()
	m.scanning = false
}

// IsOpen reports whether the overlay is shown.
func (m *Model) IsOpen() bool { return m.open }

// HasResults reports whether a past scan left navigable matches.
func (m *Model) HasResults() bool { return m.list.Total() > 0 }

// Advance moves the retained result cursor by delta (wrapping) and returns
// the match — the next/prev-match seam that works with the overlay closed.
func (m *Model) Advance(delta int) (locations.Item, bool) { return m.list.Advance(delta) }

// Query returns the current query text (replace-in-path builds on it, #86).
func (m *Model) Query() string { return m.query }

// theme returns the active palette, defaulting when none was threaded in.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// Apply consumes one streamed scan message, dropping stale generations.
func (m *Model) Apply(msg tea.Msg) {
	switch msg := msg.(type) {
	case search.BatchMsg:
		if msg.Gen != m.gen {
			return
		}
		items := make([]locations.Item, len(msg.Matches))
		for i, hit := range msg.Matches {
			items[i] = locations.Item{
				Path:     hit.Path,
				Line:     hit.Line,
				StartCol: hit.StartCol,
				EndCol:   hit.EndCol,
				Text:     hit.Text,
			}
		}
		m.list.Append(items)
	case search.DoneMsg:
		if msg.Gen != m.gen {
			return
		}
		m.scanning = false
		m.truncated = msg.Truncated
		if msg.Err != nil {
			m.errText = msg.Err.Error()
		}
	}
}

// rescan restarts the scan for the current inputs (or clears on an empty
// query).
func (m *Model) rescan() {
	m.list.Reset()
	m.truncated = false
	m.errText = ""
	if strings.TrimSpace(m.query) == "" {
		m.svc.Cancel()
		m.scanning = false
		return
	}
	m.scanning = true
	m.lastQuery = search.Query{
		Pattern:       m.query,
		Root:          m.root,
		CaseSensitive: m.caseSensitive,
		WholeWord:     m.wholeWord,
		Regex:         m.regex,
		Include:       splitGlobs(m.include),
		Exclude:       splitGlobs(m.exclude),
	}
	m.gen = m.svc.Scan(m.lastQuery)
}

// splitGlobs turns a comma-separated field into a glob slice.
func splitGlobs(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// Update handles one key while the overlay is open.
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	// Selection semantics for the remembered query (#277): the first typed
	// character replaces it; any other key keeps the text, drops the mark.
	pre := m.preselect
	m.preselect = false
	switch msg.String() {
	case "esc":
		m.Close()
		return nil
	case "enter":
		// Replace mode: enter applies the selected match and steps on;
		// find mode (and alt+enter below): open the file at the match.
		if m.replaceMode {
			if it, ok := m.list.RemoveCurrent(); ok {
				m.commitHistory()
				return m.replaceCmd([]locations.Item{it})
			}
			return nil
		}
		return m.openCurrent()
	// The ctrl chords double every alt binding below: on macOS Option is a
	// composition key, so alt chords never reach the terminal (#422, same
	// story as the tab keys in #248). ctrl is the delivered primary; alt
	// stays for terminals where it works.
	case "alt+enter", "ctrl+enter":
		return m.openCurrent()
	case "alt+f", "ctrl+f":
		// Replace every match of the selected file.
		if m.replaceMode {
			if path, items := m.list.CurrentGroup(); len(items) > 0 {
				m.commitHistory()
				batch := append([]locations.Item(nil), items...)
				m.list.RemoveGroup(path)
				return m.replaceCmd(batch)
			}
		}
		return nil
	case "alt+a", "ctrl+a":
		// Replace all matches.
		if m.replaceMode && m.list.Total() > 0 {
			m.commitHistory()
			batch := m.list.All()
			m.list.Reset()
			return m.replaceCmd(batch)
		}
		return nil
	case "tab":
		m.setFocus(m.nextField(1))
		return nil
	case "shift+tab":
		m.setFocus(m.nextField(-1))
		return nil
	case "down":
		if m.list.Total() > 0 {
			m.list.Move(1)
		} else {
			m.history(-1) // toward older is up; down walks back to newer
		}
		return nil
	case "up":
		if m.list.Total() > 0 {
			m.list.Move(-1)
		} else {
			m.history(1)
		}
		return nil
	case "pgdown":
		m.list.Move(10)
		return nil
	case "pgup":
		m.list.Move(-10)
		return nil
	case "alt+c", "ctrl+c":
		m.caseSensitive = !m.caseSensitive
		m.rescan()
		return nil
	case "alt+w", "ctrl+w":
		m.wholeWord = !m.wholeWord
		m.rescan()
		return nil
	case "alt+x", "ctrl+x":
		m.regex = !m.regex
		m.rescan()
		return nil
	case "alt+up", "ctrl+up":
		m.history(1)
		return nil
	case "alt+down", "ctrl+down":
		m.history(-1)
		return nil
	}
	// Everything else is single-line editing on the focused field (#763):
	// cursor motions, word ops, insertion. The chords above keep priority.
	f := m.focused()
	if out, ncur, handled, changed := ui.EditKey(msg, *f, m.cur); handled {
		if pre && m.focus == fieldQuery && len(out) > len(*f) {
			// The key inserted text: it replaces the selected prefill (#277),
			// so re-apply it to an empty field.
			out, ncur, _, _ = ui.EditKey(msg, "", 0)
		}
		*f, m.cur = out, ncur
		if changed {
			m.editedField()
		}
	}
	return nil
}

// setFocus moves the input focus and parks the cursor at the field's end.
func (m *Model) setFocus(f field) {
	m.focus = f
	m.cur = len([]rune(*m.focused()))
}

// layoutInfo maps content rows (0 = first row inside the border) to click
// targets; View fills it in each render, -1 marks an absent row.
type layoutInfo struct {
	query, replace, toggles, include, exclude int
	listTop, listRows                         int
}

// toggleSpans mirrors togglesRow's layout: the half-open x range of each
// toggle ("[x] " + label) within the content row.
func toggleSpans() [3][2]int {
	labels := [3]string{caseLabel, wordLabel, regexLabel}
	var spans [3][2]int
	x := len(togglesIndent)
	for i, l := range labels {
		w := 4 + len(l) // "[x] " + label
		spans[i] = [2]int{x, x + w}
		x += w + 2
	}
	return spans
}

// Click handles a left press at panel-local coordinates (0,0 = the box's
// top-left border cell, mirroring the settings panel, #127): an input row
// takes focus, a toggle flips and rescans, a result row selects its match,
// and a press on the already-selected match opens it — the same
// press-again-to-activate semantics as the settings panel.
func (m *Model) Click(x, y int) tea.Cmd {
	if !m.open || m.lay.query <= 0 {
		return nil
	}
	cx, cy := x-2, y-1 // border + horizontal padding
	if cx < 0 || cy < 0 {
		return nil
	}
	switch cy {
	case m.lay.query:
		m.setFocus(fieldQuery)
		m.preselect = false
	case m.lay.replace:
		m.setFocus(fieldReplace)
	case m.lay.include:
		m.setFocus(fieldInclude)
	case m.lay.exclude:
		m.setFocus(fieldExclude)
	case m.lay.toggles:
		for i, sp := range toggleSpans() {
			if cx < sp[0] || cx >= sp[1] {
				continue
			}
			switch i {
			case 0:
				m.caseSensitive = !m.caseSensitive
			case 1:
				m.wholeWord = !m.wholeWord
			case 2:
				m.regex = !m.regex
			}
			m.rescan()
			break
		}
	}
	if m.lay.listTop >= 0 && cy >= m.lay.listTop && cy < m.lay.listTop+m.lay.listRows {
		if idx, ok := m.list.ItemAt(cy - m.lay.listTop); ok {
			if idx == m.list.Cursor() {
				return m.openCurrent()
			}
			m.list.SetCursor(idx)
		}
	}
	return nil
}

// Wheel scrolls the results list by delta items.
func (m *Model) Wheel(delta int) { m.list.Move(delta) }

// openCurrent dispatches the selected match as a navigation and closes.
func (m *Model) openCurrent() tea.Cmd {
	it, ok := m.list.Current()
	if !ok {
		return nil
	}
	m.commitHistory()
	m.Close()
	return func() tea.Msg {
		return OpenLocationMsg{Path: it.Path, Line: it.Line, Col: it.StartCol}
	}
}

// replaceCmd dispatches an apply request for the given matches.
func (m *Model) replaceCmd(items []locations.Item) tea.Cmd {
	req := ReplaceRequestMsg{Items: items, Replacement: m.replace, Query: m.lastQuery}
	return func() tea.Msg { return req }
}

// nextField cycles input focus, skipping the replace field in find mode.
func (m *Model) nextField(dir int) field {
	f := m.focus
	for {
		f = (f + field(dir) + fieldCount) % fieldCount
		if f != fieldReplace || m.replaceMode {
			return f
		}
	}
}

// focused returns the field the cursor edits.
func (m *Model) focused() *string {
	switch m.focus {
	case fieldReplace:
		return &m.replace
	case fieldInclude:
		return &m.include
	case fieldExclude:
		return &m.exclude
	}
	return &m.query
}

// editedField reacts to a text change: query/glob changes restart the scan
// (editing the query also leaves history-recall mode); the replacement
// template only changes the preview, never the match set.
func (m *Model) editedField() {
	if m.focus == fieldReplace {
		return
	}
	if m.focus == fieldQuery {
		m.histIdx = -1
	}
	m.rescan()
}

// history walks the recall list: dir>0 moves to older entries, dir<0 back
// toward the live query.
func (m *Model) history(dir int) {
	if len(m.hist) == 0 || m.focus != fieldQuery {
		return
	}
	next := m.histIdx + dir
	if next < -1 {
		next = -1
	}
	if next >= len(m.hist) {
		next = len(m.hist) - 1
	}
	if next == m.histIdx {
		return
	}
	m.histIdx = next
	if next == -1 {
		m.query = ""
	} else {
		m.query = m.hist[next]
	}
	m.cur = len([]rune(m.query))
	m.rescan()
}

// commitHistory records the current query at the front of the recall list.
func (m *Model) commitHistory() {
	q := strings.TrimSpace(m.query)
	if q == "" {
		return
	}
	out := []string{q}
	for _, h := range m.hist {
		if h != q {
			out = append(out, h)
		}
	}
	if len(out) > maxHistory {
		out = out[:maxHistory]
	}
	m.hist = out
}

// View renders the centered overlay box.
func (m *Model) View() string {
	if !m.open || m.width <= 0 {
		return ""
	}
	pal := m.theme()
	boxW := m.width - 12
	if boxW > 100 {
		boxW = 100
	}
	if boxW < 40 {
		boxW = min(40, m.width-2)
	}
	innerW := boxW - 4 // border + padding

	heading := "Find in Path"
	if m.replaceMode {
		heading = "Replace in Path"
	}
	title := lipgloss.NewStyle().Bold(true).Underline(true).Render(heading)
	lay := layoutInfo{replace: -1, listTop: -1}
	rows := []string{title, ""}
	lay.query = len(rows)
	rows = append(rows, m.inputRow("Search ", m.query, fieldQuery, innerW))
	if m.replaceMode {
		lay.replace = len(rows)
		rows = append(rows, m.inputRow("Replace", m.replace, fieldReplace, innerW))
	}
	lay.toggles = len(rows)
	rows = append(rows, m.togglesRow(innerW))
	lay.include = len(rows)
	rows = append(rows, m.inputRow("Include", m.include, fieldInclude, innerW))
	lay.exclude = len(rows)
	rows = append(rows, m.inputRow("Exclude", m.exclude, fieldExclude, innerW))
	rows = append(rows, "")

	listH := m.height/2 - 9
	if listH < 4 {
		listH = 4
	}
	if body := m.list.Render(innerW, listH, pal, m.displayPath); body != "" {
		lay.listTop = len(rows)
		lay.listRows = strings.Count(body, "\n") + 1
		rows = append(rows, body)
	}
	m.lay = lay
	if pre := m.previewRows(innerW); len(pre) > 0 {
		rows = append(rows, "")
		rows = append(rows, pre...)
	}
	rows = append(rows, "", m.statusRow(innerW))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1).
		Width(boxW - 2)
	return box.Render(strings.Join(rows, "\n"))
}

// inputRow renders one labelled input line with a block cursor on the focused
// field.
func (m *Model) inputRow(label, value string, f field, width int) string {
	pal := m.theme()
	lab := lipgloss.NewStyle().Faint(true).Render(label + " ")
	text := value
	switch {
	case f == fieldQuery && m.preselect && value != "":
		// The remembered query is "selected" (#277): render it inverted so
		// it reads as replace-on-type.
		text = lipgloss.NewStyle().Reverse(true).Render(value)
		if m.focus == f {
			text += lipgloss.NewStyle().Reverse(true).Render(" ")
		}
	case m.focus == f:
		text = ui.CursorView(value, m.cur)
	}
	row := lab + text
	if m.focus == f {
		row = lipgloss.NewStyle().Foreground(pal.Foreground).Render(lab) + text
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(row)
}

// The toggle row's fixed pieces; toggleSpans derives the click ranges from
// them, so keep the render and the constants in sync.
const (
	togglesIndent = "        "
	caseLabel     = "Case (ctrl+c)"
	wordLabel     = "Word (ctrl+w)"
	regexLabel    = "Regex (ctrl+x)"
)

// togglesRow renders the three match-mode toggles with their key hints
// (ctrl is the delivered primary on macOS, #422; alt still works elsewhere).
func (m *Model) togglesRow(width int) string {
	pal := m.theme()
	on := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true)
	off := lipgloss.NewStyle().Faint(true)
	part := func(label string, active bool) string {
		if active {
			return on.Render("[x] " + label)
		}
		return off.Render("[ ] " + label)
	}
	row := togglesIndent + part(caseLabel, m.caseSensitive) +
		"  " + part(wordLabel, m.wholeWord) +
		"  " + part(regexLabel, m.regex)
	return lipgloss.NewStyle().MaxWidth(width).Render(row)
}

// previewRows renders the before/after preview for the selected match in
// replace mode: the current line and what applying the template makes of it.
func (m *Model) previewRows(width int) []string {
	if !m.replaceMode {
		return nil
	}
	it, ok := m.list.Current()
	if !ok {
		return nil
	}
	after, valid := search.RewriteRange(it.Text, it.StartCol, it.EndCol, m.lastQuery, m.replace)
	if !valid {
		return nil
	}
	pal := m.theme()
	clip := lipgloss.NewStyle().MaxWidth(width)
	del := lipgloss.NewStyle().Foreground(pal.Error)
	add := lipgloss.NewStyle().Foreground(pal.Success)
	return []string{
		clip.Render(del.Render("- " + strings.TrimRight(it.Text, " \t"))),
		clip.Render(add.Render("+ " + strings.TrimRight(after, " \t"))),
	}
}

// statusRow summarizes the scan: live progress, counts, truncation, errors.
func (m *Model) statusRow(width int) string {
	pal := m.theme()
	dim := lipgloss.NewStyle().Faint(true)
	switch {
	case m.errText != "":
		return lipgloss.NewStyle().Foreground(pal.Error).Render(
			lipgloss.NewStyle().MaxWidth(width).Render("error: " + m.errText))
	case strings.TrimSpace(m.query) == "":
		if m.replaceMode {
			return dim.Render("type to search — enter replaces match, ctrl+f file, ctrl+a all, ctrl+enter opens")
		}
		return dim.Render("type to search — enter opens, esc closes, tab cycles fields")
	case m.scanning && m.list.Total() == 0:
		return dim.Render("searching…")
	case m.list.Total() == 0:
		return dim.Render("no matches")
	}
	s := plural(m.list.Total(), "match", "matches") + " in " + plural(m.list.Files(), "file", "files")
	if m.truncated {
		s += " (truncated)"
	} else if m.scanning {
		s += "…"
	}
	return dim.Render(s)
}

// plural renders "1 match" / "3 matches" style counts.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

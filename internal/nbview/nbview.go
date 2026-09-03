// Package nbview is the read-only Jupyter notebook viewer pane (#2425): an
// .ipynb file rendered as its cells rather than as the JSON it is stored in.
// Markdown cells go through the markdown preview's renderer, code cells are
// highlighted under the notebook's own language, and each cell's outputs are
// shown below it — stream and text/plain as text, text/html degraded to text,
// image/png and image/jpeg as pixels through the same Kitty graphics path the
// image pane uses, errors in the error colour with their traceback.
//
// Execution and editing are out of scope. The split is deliberate: notebook.go
// owns the nbformat model with no rendering in it, this file owns the pane, so
// a later edit mode grows on the same model instead of replacing it.
package nbview

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/highlight"
	"ike/internal/lang"
	"ike/internal/theme"
	"ike/internal/ui"
)

// maxHighlightBytes caps the source a single code cell is highlighted for.
// A cell holding a megabyte of generated data is not code anybody reads
// coloured, and the parse would stall the render loop.
const maxHighlightBytes = 128 << 10

// CopyMsg asks the root model to place text on the system clipboard: the pane
// emits it, the app owns the clipboard, exactly like the hex viewer's copy.
type CopyMsg struct {
	Text string
	What string // human label for the "copied …" notice
}

// ScratchMsg asks the root model to open a cell's source as a scratch file
// (#2425): the pane knows the language, the app owns the scratch store and
// the open funnel.
type ScratchMsg struct {
	Ext     string
	Content string
}

// SaveImageMsg asks the root model to write an image output to disk. Path is
// the suggested destination next to the notebook; the app resolves collisions
// and reports where the bytes landed.
type SaveImageMsg struct {
	Path string
	Data []byte
}

// rowKind says what one rendered row is, which decides its styling and
// whether folding hides it.
type rowKind int

const (
	rowSource rowKind = iota // rendered markdown or highlighted source
	rowOutput                // an output's text
	rowError                 // an error output's name/value/traceback
	rowImage                 // one placeholder line of an image output
	rowNote                  // a viewer-generated note (fold marker, labels)
	rowGap                   // the blank line between cells
)

// row is one rendered line: the styled text the pane draws, the plain text a
// search match re-renders with the match background, and the coordinates that
// let navigation, folding and search address it.
type row struct {
	text  string
	plain string
	kind  rowKind
	cell  int // owning cell index, -1 for a gap
	first bool
	// src is the 0-based line inside the cell's source for a rowSource of a
	// code cell, -1 otherwise. Markdown rows are rendered output, not source
	// lines, so they carry -1 and match by cell.
	src int
}

// Model is one notebook viewer pane bound to a file path.
type Model struct {
	key  string
	path string
	pal  *theme.Palette

	nb  Notebook
	err error // read or parse failure; the pane explains itself and offers "open as text"

	w, h    int
	focused bool

	rows []row
	top  int // first visible row
	cur  int // cursor cell index

	// folded holds the cells whose outputs are collapsed. A map so value
	// copies of the model share it, like the preview's image cache.
	folded map[int]bool

	// search line state, the shape the other viewer panes share.
	sEditing bool
	sInput   string
	sCur     int
	sErr     string
	query    string  // applied query, "" while no search applies
	matches  []match // matching cell/source-line positions in reading order
	mIdx     int     // index of the current match, -1 for none
	wrapped  bool

	// images holds the decoded image outputs keyed by cell/output index, and
	// the terminal's Kitty graphics capability as pushed by the app.
	images map[imgKey]*cellImage
	gfx    bool

	// hl is the capture→style table, rebuilt when the palette changes.
	hl     highlight.Theme
	hlName string
	hlOK   bool
}

// match is one search hit: the cell and, for a code cell, the source line
// inside it.
type match struct {
	cell int
	line int
}

// New reads and parses the notebook at path. Read and parse errors are kept
// for View — the pane opens either way and explains itself.
func New(key, path string, pal *theme.Palette) Model {
	m := Model{key: key, path: path, pal: pal, mIdx: -1,
		folded: map[int]bool{}, images: map[imgKey]*cellImage{}}
	m.load()
	return m
}

// load re-reads the file into the model, keeping the fold set and the cursor
// where they were: a re-render after an external change must not scroll the
// reader back to the top.
func (m *Model) load() {
	data, err := os.ReadFile(m.path)
	if err != nil {
		m.nb, m.err = Notebook{}, err
		m.rows = nil
		return
	}
	nb, err := Parse(data)
	if err != nil {
		m.nb, m.err = Notebook{}, err
		m.rows = nil
		return
	}
	m.nb, m.err = nb, nil
	if m.cur >= len(m.nb.Cells) {
		m.cur = max(0, len(m.nb.Cells)-1)
	}
	m.applySearchSet()
	m.render()
}

// Reload re-reads the notebook after the watcher reported the file changed
// (#2425) and reports whether the pane still shows content.
func (m *Model) Reload() { m.load() }

// Key returns the pane's stable registry key.
func (m *Model) Key() string { return m.key }

// Path returns the viewed notebook's path.
func (m *Model) Path() string { return m.path }

// Err returns the read or parse error, nil when the notebook rendered.
func (m *Model) Err() error { return m.err }

// Cells returns the parsed cells (tests, status line).
func (m *Model) Cells() []Cell { return m.nb.Cells }

// Cursor returns the index of the cell the cursor is on (tests, status line).
func (m *Model) Cursor() int { return m.cur }

// Lang returns the notebook's language id, "" when it declares none.
func (m *Model) Lang() string { return m.nb.Lang }

// Folded reports whether the cell's outputs are collapsed (tests).
func (m *Model) Folded(cell int) bool { return m.folded[cell] }

// Matches returns the search matches in reading order (tests).
func (m *Model) Matches() []match { return m.matches }

// Searching reports whether the search line is open (tests).
func (m *Model) Searching() bool { return m.sEditing }

// Rows returns the rendered row texts (tests): the pane body without the
// scroll window or the footer.
func (m *Model) Rows() []string {
	out := make([]string, len(m.rows))
	for i, r := range m.rows {
		out[i] = r.plain
	}
	return out
}

// Close releases the decoded image pixels. A zero-value model (after a tab
// detach moved the live one) holds nothing.
func (m *Model) Close() { m.images = map[imgKey]*cellImage{} }

// SetSize records the pane interior and re-renders: both the markdown and the
// image placements are width-bound.
func (m *Model) SetSize(w, h int) {
	if w == m.w && h == m.h {
		return
	}
	m.w, m.h = w, h
	m.render()
}

// SetFocused records focus for the chrome.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the theme palette and re-renders in the new style.
func (m *Model) SetPalette(p *theme.Palette) {
	m.pal = p
	m.hlOK = false
	m.render()
}

// palette returns the palette the pane styles against, falling back to the
// default for zero-value models (tests).
func (m *Model) palette() *theme.Palette {
	if m.pal == nil {
		return theme.DefaultPalette()
	}
	return m.pal
}

// bodyRows is how many rows of the document are visible: the interior minus
// the footer line.
func (m *Model) bodyRows() int { return max(1, m.h-1) }

// Update handles one key press. Everything the pane owns is a key; the app
// routes clipboard, scratch and image-save work back out as messages.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if m.err != nil {
		return nil
	}
	if m.sEditing {
		return m.searchKey(key)
	}
	page := m.bodyRows()
	switch key.String() {
	case "j":
		m.moveCell(1)
	case "k":
		m.moveCell(-1)
	case "down", "ctrl+e":
		m.scrollBy(1)
	case "up", "ctrl+y":
		m.scrollBy(-1)
	case "pgdown", "ctrl+f":
		m.scrollBy(page)
	case "pgup", "ctrl+b":
		m.scrollBy(-page)
	case "ctrl+d":
		m.scrollBy(page / 2)
	case "ctrl+u":
		m.scrollBy(-page / 2)
	case "g", "home":
		m.gotoCell(0)
	case "G", "end":
		m.gotoCell(len(m.nb.Cells) - 1)
	case "enter", " ":
		m.toggleFold()
	case "esc":
		if m.query != "" {
			m.query, m.matches, m.mIdx, m.wrapped = "", nil, -1, false
		}
	case "/":
		m.startSearch()
	case "n":
		m.stepMatch(1)
	case "N":
		m.stepMatch(-1)
	case "e":
		return m.scratchCmd()
	case "y", "cmd+c", "super+c":
		return m.copyCmd()
	case "o":
		return m.saveImageCmd()
	}
	return nil
}

// PasteText inserts a paste into the open search line (#2002); with the line
// closed the text is dropped rather than leaking into navigation keys.
func (m *Model) PasteText(text string) {
	if !m.sEditing || text == "" {
		return
	}
	r := []rune(m.sInput)
	m.sInput = string(r[:m.sCur]) + text + string(r[m.sCur:])
	m.sCur += len([]rune(text))
	m.sErr = ""
}

// Wheel scrolls the document by delta rows.
func (m *Model) Wheel(delta int) { m.scrollBy(delta) }

// scrollBy moves the viewport, leaving the cell cursor where it is: the
// cursor addresses a cell, the scroll addresses rows, and a reader scrolling
// past a cell should not silently retarget `e` or `y`.
func (m *Model) scrollBy(delta int) {
	m.top += delta
	m.clampScroll()
}

// clampScroll keeps the viewport inside the document.
func (m *Model) clampScroll() {
	maxTop := max(0, len(m.rows)-m.bodyRows())
	m.top = min(max(0, m.top), maxTop)
}

// moveCell steps the cell cursor and scrolls the new cell into view.
func (m *Model) moveCell(delta int) { m.gotoCell(m.cur + delta) }

// gotoCell puts the cursor on a cell, clamped to the document, and reveals it.
func (m *Model) gotoCell(i int) {
	if len(m.nb.Cells) == 0 {
		return
	}
	m.cur = min(max(0, i), len(m.nb.Cells)-1)
	m.revealCell(m.cur)
}

// revealCell scrolls so the cell's first row is visible, showing as much of
// the cell as fits below it.
func (m *Model) revealCell(cell int) {
	start, end := m.cellRows(cell)
	if start < 0 {
		return
	}
	if start < m.top {
		m.top = start
	} else if end >= m.top+m.bodyRows() {
		// Prefer showing the cell's head over its tail: a long cell scrolls
		// to its first row rather than to its last.
		m.top = min(start, end-m.bodyRows()+1)
	}
	m.clampScroll()
}

// cellRows returns the first and last row index of a cell, (-1, -1) when the
// cell rendered nothing.
func (m *Model) cellRows(cell int) (start, end int) {
	start, end = -1, -1
	for i, r := range m.rows {
		if r.cell != cell || r.kind == rowGap {
			continue
		}
		if start < 0 {
			start = i
		}
		end = i
	}
	return start, end
}

// toggleFold collapses or expands the cursor cell's outputs. A cell without
// outputs says so rather than silently doing nothing.
func (m *Model) toggleFold() {
	if m.cur >= len(m.nb.Cells) || len(m.nb.Cells[m.cur].Outputs) == 0 {
		return
	}
	if m.folded == nil {
		m.folded = map[int]bool{}
	}
	m.folded[m.cur] = !m.folded[m.cur]
	m.render()
	m.revealCell(m.cur)
}

// cellExt is the file extension a cell's source belongs under: the
// notebook's declared file_extension, else the registered extension of its
// language id, else "txt". A markdown cell is markdown whatever the kernel is.
func (m *Model) cellExt(c Cell) string {
	if c.Type == CellMarkdown {
		return "md"
	}
	if m.nb.LangExt != "" {
		return m.nb.LangExt
	}
	if l, ok := lang.ByID(strings.ToLower(m.nb.Lang)); ok && len(l.Extensions) > 0 {
		return l.Extensions[0]
	}
	return "txt"
}

// scratchCmd emits the "open this cell in a scratch" request for the cursor
// cell (#2425), so the source can be edited, run or kept without the
// notebook's JSON around it.
func (m *Model) scratchCmd() tea.Cmd {
	if m.cur >= len(m.nb.Cells) {
		return nil
	}
	c := m.nb.Cells[m.cur]
	if strings.TrimSpace(c.Source) == "" {
		return nil
	}
	msg := ScratchMsg{Ext: m.cellExt(c), Content: c.Source + "\n"}
	return func() tea.Msg { return msg }
}

// copyCmd emits the cursor cell's source for the clipboard.
func (m *Model) copyCmd() tea.Cmd {
	if m.cur >= len(m.nb.Cells) {
		return nil
	}
	c := m.nb.Cells[m.cur]
	if c.Source == "" {
		return nil
	}
	msg := CopyMsg{Text: c.Source, What: fmt.Sprintf("cell %d source", m.cur+1)}
	return func() tea.Msg { return msg }
}

// saveImageCmd emits the save request for the first image output of the
// cursor cell, with a destination suggested next to the notebook.
func (m *Model) saveImageCmd() tea.Cmd {
	if m.cur >= len(m.nb.Cells) {
		return nil
	}
	for _, o := range m.nb.Cells[m.cur].Outputs {
		if !o.HasImage() {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(m.path), filepath.Ext(m.path))
		name := fmt.Sprintf("%s-cell%d.%s", base, m.cur+1, o.ImageExt())
		msg := SaveImageMsg{Path: filepath.Join(filepath.Dir(m.path), name), Data: o.Image}
		return func() tea.Msg { return msg }
	}
	return nil
}

// startSearch opens the search line, seeded with the applied query.
func (m *Model) startSearch() {
	if m.err != nil || len(m.nb.Cells) == 0 {
		return
	}
	m.sEditing = true
	m.sCur = len([]rune(m.sInput))
	m.sErr = ""
}

// OpenSearch implements the pane's Searchable capability (#2409): cmd+f opens
// the same line "/" does.
func (m *Model) OpenSearch() bool {
	m.startSearch()
	return m.sEditing
}

// NextMatch implements the pane's match-step capability (#2410).
func (m *Model) NextMatch() ui.MatchStep { return m.searchStep(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.searchStep(-1) }

// searchStep serves cmd+g / cmd+shift+g while the search line is open: it
// applies the typed query if it changed and steps the current match.
func (m *Model) searchStep(delta int) ui.MatchStep {
	if !m.sEditing {
		return ui.NoStep
	}
	if !m.applySearch() || len(m.matches) == 0 {
		return ui.NoMatches()
	}
	m.stepMatch(delta)
	return ui.Stepped(m.mIdx, len(m.matches), m.wrapped)
}

// searchKey feeds one key to the open search line, which owns the keyboard.
func (m *Model) searchKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc":
		m.sEditing, m.sErr = false, ""
	case "enter":
		if m.applySearch() {
			m.sEditing = false
		}
	default:
		if out, ncur, handled, changed := ui.EditKey(key, m.sInput, m.sCur); handled {
			m.sInput, m.sCur = out, ncur
			if changed {
				m.sErr, m.wrapped = "", false
			}
		}
	}
	return nil
}

// applySearch runs the typed query over the cell sources and jumps to the
// first match at or after the cursor cell. It reports false on an empty
// query — the one input that is not a search.
func (m *Model) applySearch() bool {
	q := strings.TrimSpace(m.sInput)
	if q == "" {
		m.sErr = "empty search"
		return false
	}
	if q != m.query {
		m.query, m.mIdx, m.wrapped = q, -1, false
		m.applySearchSet()
	}
	if len(m.matches) == 0 {
		m.sErr = "no matches"
		return true
	}
	m.sErr = ""
	if m.mIdx < 0 {
		m.mIdx = 0
		for i, mt := range m.matches {
			if mt.cell >= m.cur {
				m.mIdx = i
				break
			}
		}
		m.jumpToMatch()
	}
	return true
}

// applySearchSet recomputes the match set for the applied query. The search
// is over cell *sources* (#2425) — what the author wrote, not what an output
// happened to print — and is case-insensitive, like the other panes' filters.
func (m *Model) applySearchSet() {
	m.matches = nil
	if m.query == "" {
		return
	}
	needle := strings.ToLower(m.query)
	for ci, c := range m.nb.Cells {
		for li, line := range strings.Split(c.Source, "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				m.matches = append(m.matches, match{cell: ci, line: li})
			}
		}
	}
	if m.mIdx >= len(m.matches) {
		m.mIdx = len(m.matches) - 1
	}
}

// stepMatch moves the current match by delta, wrapping, and scrolls to it.
func (m *Model) stepMatch(delta int) {
	if len(m.matches) == 0 {
		return
	}
	if m.mIdx < 0 {
		m.mIdx = 0
	} else {
		next := m.mIdx + delta
		m.wrapped = next < 0 || next >= len(m.matches)
		m.mIdx = (next%len(m.matches) + len(m.matches)) % len(m.matches)
	}
	m.jumpToMatch()
}

// jumpToMatch puts the cell cursor on the current match and scrolls its row
// into view.
func (m *Model) jumpToMatch() {
	if m.mIdx < 0 || m.mIdx >= len(m.matches) {
		return
	}
	mt := m.matches[m.mIdx]
	m.cur = min(mt.cell, max(0, len(m.nb.Cells)-1))
	if r := m.matchRow(mt); r >= 0 {
		if r < m.top || r >= m.top+m.bodyRows() {
			m.top = max(0, r-m.bodyRows()/2)
			m.clampScroll()
		}
		return
	}
	m.revealCell(m.cur)
}

// matchRow returns the rendered row a match points at, -1 when the source
// line has no row of its own (a markdown cell, whose rows are rendered
// output rather than source lines).
func (m *Model) matchRow(mt match) int {
	for i, r := range m.rows {
		if r.cell == mt.cell && r.kind == rowSource && r.src == mt.line {
			return i
		}
	}
	return -1
}

// isMatchRow reports whether a rendered row is one of the search matches.
func (m *Model) isMatchRow(r row) bool {
	if len(m.matches) == 0 || r.kind != rowSource {
		return false
	}
	for _, mt := range m.matches {
		if mt.cell != r.cell {
			continue
		}
		// A markdown cell's rows are rendered output, so the whole cell
		// highlights; a code cell matches line for line.
		if r.src < 0 || r.src == mt.line {
			return true
		}
	}
	return false
}

// currentMatchRow returns the row of the current match, -1 when none.
func (m *Model) currentMatchRow() int {
	if m.mIdx < 0 || m.mIdx >= len(m.matches) {
		return -1
	}
	return m.matchRow(m.matches[m.mIdx])
}

// View renders the pane interior: the visible rows plus the footer.
func (m *Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	if m.err != nil {
		return m.errorView()
	}
	pal := m.palette()
	gw := m.gutterWidth()
	cursorRow := m.currentMatchRow()
	lines := make([]string, 0, m.h)
	for i := 0; i < m.bodyRows(); i++ {
		ix := m.top + i
		if ix >= len(m.rows) {
			lines = append(lines, "")
			continue
		}
		r := m.rows[ix]
		body := r.text
		if m.isMatchRow(r) {
			st := lipgloss.NewStyle().Background(pal.OccurrenceRead).Foreground(pal.Foreground)
			if ix == cursorRow {
				st = st.Bold(true)
			}
			body = st.Render(clipTo(r.plain, max(1, m.w-gw)))
		}
		lines = append(lines, m.gutter(r, gw)+body)
	}
	lines = append(lines, m.footer())
	return strings.Join(lines, "\n")
}

// errorView is the body of a notebook that could not be read or parsed
// (#2425): the reason, and the way out — the JSON itself is still openable.
func (m *Model) errorView() string {
	pal := m.palette()
	dim := lipgloss.NewStyle().Foreground(pal.Ghost)
	body := lipgloss.NewStyle().Bold(true).Foreground(pal.Foreground).Render(filepath.Base(m.path)) +
		"\n" + lipgloss.NewStyle().Foreground(pal.Error).Render(m.err.Error()) +
		"\n\n" + dim.Render("not a readable nbformat 4 notebook — open it as JSON with Open File As… → Text editor")
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, body)
}

// gutter renders one row's left column: the cell's index and type on its
// first row, a bar on the rest, in the cursor cell's accent.
func (m *Model) gutter(r row, gw int) string {
	pal := m.palette()
	if r.kind == rowGap || r.cell < 0 {
		return strings.Repeat(" ", gw)
	}
	label := ""
	if r.first {
		label = m.cellLabel(r.cell)
	}
	st := lipgloss.NewStyle().Foreground(pal.Ghost)
	if r.cell == m.cur {
		// The cell cursor is only emphasised while the pane has the keyboard:
		// an unfocused pane's accent would read as "this is where you are".
		st = lipgloss.NewStyle().Foreground(pal.Accent)
		if m.focused {
			st = st.Bold(true)
		}
	}
	pad := gw - 2 - len([]rune(label))
	if pad < 0 {
		label, pad = string([]rune(label)[:max(0, gw-2)]), 0
	}
	return st.Render(label+strings.Repeat(" ", pad)+"│") + " "
}

// cellLabel is the gutter text of a cell: its 1-based index, its type, and a
// code cell's execution count — `[3]` when it ran, `[ ]` when it never did.
func (m *Model) cellLabel(i int) string {
	if i < 0 || i >= len(m.nb.Cells) {
		return ""
	}
	c := m.nb.Cells[i]
	kind := "raw"
	switch c.Type {
	case CellMarkdown:
		kind = "md"
	case CellCode:
		kind = "code"
	}
	label := fmt.Sprintf("%d %s", i+1, kind)
	if c.Type == CellCode {
		if c.ExecCount > 0 {
			label += fmt.Sprintf(" [%d]", c.ExecCount)
		} else {
			label += " [ ]"
		}
	}
	return label
}

// gutterWidth is the width of the gutter column including its bar and the
// space after it, sized to the widest cell label and capped so a notebook
// with four-digit execution counts cannot eat the pane.
func (m *Model) gutterWidth() int {
	w := 0
	for i := range m.nb.Cells {
		if n := len([]rune(m.cellLabel(i))); n > w {
			w = n
		}
	}
	return min(max(w+2, 6), 18)
}

// footer renders the bottom line: the open search line, or the position and
// the key hints.
func (m *Model) footer() string {
	pal := m.palette()
	if m.sEditing {
		if m.sErr != "" {
			return lipgloss.NewStyle().Foreground(pal.Error).Render(clipTo(" /"+m.sInput+" — "+m.sErr, m.w))
		}
		hint := " enter search · esc close · matches cell sources"
		if len(m.matches) > 0 {
			hint = fmt.Sprintf(" %d/%d ·%s", m.mIdx+1, len(m.matches), hint)
		}
		return lipgloss.NewStyle().Faint(true).Render(clipTo(" /"+m.sInput+" ·"+hint, m.w))
	}
	status := fmt.Sprintf(" cell %d/%d", m.cur+1, len(m.nb.Cells))
	if m.nb.Lang != "" {
		status += " · " + m.nb.Lang
	}
	if m.nb.Format != "" {
		status += " · nbformat " + m.nb.Format
	}
	if len(m.matches) > 0 {
		status += fmt.Sprintf(" · match %d/%d", m.mIdx+1, len(m.matches))
	}
	hints := "j/k cell · enter fold outputs · / search · e scratch · y copy · o save image · g/G ends"
	return lipgloss.NewStyle().Faint(true).Render(clipTo(status+" · "+hints, m.w))
}

// clipTo cuts s to width cells, respecting the styling already in it; the
// footer never wraps.
func clipTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "")
}

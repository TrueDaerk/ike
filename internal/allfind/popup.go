package allfind

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/search"
	"ike/internal/theme"
	"ike/internal/ui"
)

// popup.go is the all-projects results surface (#2394): a floating box that
// appears when the background scan finishes WITHOUT taking the keyboard — the
// editor keeps typing; a keybind, a mouse click or the show-results command
// focuses it. Matches are grouped by project, then by file. Esc hides the box
// (results stay for the show-results command); enter on a match dispatches
// OpenMatchMsg, which the root model routes to a plain open (current project)
// or a project switch followed by the open.

// OpenMatchMsg asks the root model to open a selected match: Path at the
// 1-based Line and 0-based rune Col, in the project rooted at Root. A Root
// other than the current project switches first (#2394).
type OpenMatchMsg struct {
	Root string
	Path string
	Line int
	Col  int
}

// popItem is one match row.
type popItem struct {
	Path     string
	Line     int
	StartCol int
	EndCol   int
	Text     string
}

// fileGroup is one file's matches within a project group.
type fileGroup struct {
	path  string
	items []popItem
}

// projGroup is one searched project's result block.
type projGroup struct {
	root  string
	name  string
	files []fileGroup
	count int
}

// Popup is the results state. It is session state — the root model carries it
// across project switches like the notification history (#1514), because the
// results deliberately span projects.
type Popup struct {
	visible bool
	focused bool

	scanning  bool
	groups    []projGroup
	names     map[string]string // root → display name, seeded by Begin
	searched  int               // number of roots the scan covered
	total     int
	truncated bool
	errs      map[string]error
	query     string

	cursor int // index into the flattened match rows
	scroll int // first visible content row of the body

	width, height int
	pal           *theme.Palette
}

// NewPopup returns an empty, hidden popup.
func NewPopup() *Popup { return &Popup{} }

// SetPalette threads the active theme in.
func (p *Popup) SetPalette(pal *theme.Palette) { p.pal = pal }

// SetSize records the terminal size.
func (p *Popup) SetSize(w, h int) { p.width, p.height = w, h }

// Begin resets the popup for a new scan over the given projects. The box
// stays hidden until the scan finishes — only a small progress notification
// runs meanwhile, so the user is never interrupted.
func (p *Popup) Begin(query string, roots []Project) {
	p.groups = nil
	p.names = map[string]string{}
	for _, r := range roots {
		p.names[r.Root] = r.Name
	}
	p.searched = len(roots)
	p.total = 0
	p.truncated = false
	p.errs = nil
	p.query = query
	p.scanning = true
	p.cursor = 0
	p.scroll = 0
	p.visible = false
	p.focused = false
}

// Append records one streamed batch from root.
func (p *Popup) Append(root string, matches []search.Match) {
	g := p.group(root)
	for _, m := range matches {
		if len(g.files) == 0 || g.files[len(g.files)-1].path != m.Path {
			g.files = append(g.files, fileGroup{path: m.Path})
		}
		f := &g.files[len(g.files)-1]
		f.items = append(f.items, popItem{
			Path: m.Path, Line: m.Line, StartCol: m.StartCol, EndCol: m.EndCol, Text: m.Text,
		})
		g.count++
		p.total++
	}
}

// group finds or creates the project group for root.
func (p *Popup) group(root string) *projGroup {
	for i := range p.groups {
		if p.groups[i].root == root {
			return &p.groups[i]
		}
	}
	name := p.names[root]
	if name == "" {
		name = filepath.Base(root)
	}
	p.groups = append(p.groups, projGroup{root: root, name: name})
	return &p.groups[len(p.groups)-1]
}

// Finish ends the scan and shows the box — visible, not focused.
func (p *Popup) Finish(truncated bool, errs map[string]error) {
	p.scanning = false
	p.truncated = truncated
	p.errs = errs
	p.visible = true
	p.focused = false
	p.clampCursor()
}

// Scanning reports whether a scan is still streaming in.
func (p *Popup) Scanning() bool { return p.scanning }

// Visible reports whether the box is on screen.
func (p *Popup) Visible() bool { return p.visible }

// Focused reports whether the box owns the keyboard.
func (p *Popup) Focused() bool { return p.visible && p.focused }

// HasResults reports whether a finished scan left anything to show — matches,
// or at least a header worth re-opening (errors, empty result).
func (p *Popup) HasResults() bool { return !p.scanning && (p.total > 0 || p.searched > 0) }

// Show puts the box on screen without focusing it.
func (p *Popup) Show() { p.visible = true }

// Hide takes the box off screen; results stay for the show-results command.
func (p *Popup) Hide() { p.visible, p.focused = false, false }

// Focus shows the box and hands it the keyboard.
func (p *Popup) Focus() { p.visible, p.focused = true, true }

// Blur keeps the box visible but returns the keyboard.
func (p *Popup) Blur() { p.focused = false }

// Total returns the merged match count.
func (p *Popup) Total() int { return p.total }

// matchAt returns the idx-th match row across groups.
func (p *Popup) matchAt(idx int) (projGroup, popItem, bool) {
	for _, g := range p.groups {
		for _, f := range g.files {
			if idx < len(f.items) {
				return g, f.items[idx], true
			}
			idx -= len(f.items)
		}
	}
	return projGroup{}, popItem{}, false
}

func (p *Popup) clampCursor() {
	if p.cursor >= p.total {
		p.cursor = p.total - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// Update handles one key while the popup is focused.
func (p *Popup) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		p.Hide()
		return nil
	case "up", "k":
		p.cursor = ui.StepIndex(p.cursor, -1, p.total)
		return nil
	case "down", "j":
		p.cursor = ui.StepIndex(p.cursor, 1, p.total)
		return nil
	case "pgup":
		p.cursor -= p.pageRows()
		p.clampCursor()
		return nil
	case "pgdown":
		p.cursor += p.pageRows()
		p.clampCursor()
		return nil
	case "home":
		p.cursor = 0
		return nil
	case "end":
		p.cursor = p.total - 1
		p.clampCursor()
		return nil
	case "enter":
		return p.openCurrent()
	}
	return nil
}

// openCurrent dispatches the selected match. The box hides but keeps its
// results — coming back from the jump re-opens where it left off.
func (p *Popup) openCurrent() tea.Cmd {
	g, it, ok := p.matchAt(p.cursor)
	if !ok {
		return nil
	}
	p.Hide()
	msg := OpenMatchMsg{Root: g.root, Path: it.Path, Line: it.Line, Col: it.StartCol}
	return func() tea.Msg { return msg }
}

// pageRows is one page of body rows for pgup/pgdown.
func (p *Popup) pageRows() int {
	h := p.bodyHeight()
	if h < 1 {
		return 1
	}
	return h
}

// boxWidth is the popup's outer width.
func (p *Popup) boxWidth() int {
	w := p.width - 8
	if w > 96 {
		w = 96
	}
	if w < 30 {
		w = min(30, p.width-2)
	}
	return w
}

// bodyHeight bounds the scrollable content rows.
func (p *Popup) bodyHeight() int {
	h := p.height/2 - 6
	if h > 24 {
		h = 24
	}
	if h < 4 {
		h = 4
	}
	return h
}

// Rect reports the box's placement (top-left and size) on the screen: bottom
// right, above the status line and the toast stack's first row, so the
// non-focused box covers as little of the editing surface as possible.
func (p *Popup) Rect() (x, y, w, h int) {
	if !p.visible || p.width <= 0 {
		return 0, 0, 0, 0
	}
	v := p.View()
	w, h = lipgloss.Width(v), lipgloss.Height(v)
	x = p.width - w - 1
	if x < 0 {
		x = 0
	}
	y = p.height - h - 1
	if y < 0 {
		y = 0
	}
	return x, y, w, h
}

// Contains reports whether the screen cell (sx, sy) lies inside the box.
func (p *Popup) Contains(sx, sy int) bool {
	x, y, w, h := p.Rect()
	return p.visible && w > 0 && sx >= x && sx < x+w && sy >= y && sy < y+h
}

// Click handles a left press at screen coordinates: a press on a match row
// selects it, a second press on the selected row opens it. The caller focuses
// the box first (mouse focuses, like the popup terminal layer).
func (p *Popup) Click(sx, sy int) tea.Cmd {
	x, y, _, _ := p.Rect()
	cy := sy - y - 1 // border
	row := cy - p.headerRows()
	if row < 0 {
		return nil
	}
	_ = sx - x
	// Walk the flattened body rows to the clicked one.
	idx, ok := p.matchAtBodyRow(row + p.scroll)
	if !ok {
		return nil
	}
	if idx == p.cursor {
		return p.openCurrent()
	}
	p.cursor = idx
	return nil
}

// Wheel scrolls the body by delta rows.
func (p *Popup) Wheel(delta int) {
	p.scroll += delta
	if max := p.rowCount() - p.bodyHeight(); p.scroll > max {
		p.scroll = max
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
}

// bodyRow describes one rendered content row of the grouped list.
type bodyRow struct {
	kind  int // 0 = project header, 1 = file header, 2 = match
	group int // index into p.groups
	file  int
	match int // flattened match index (kind 2)
}

// rows flattens the grouped results into renderable rows.
func (p *Popup) rows() []bodyRow {
	var out []bodyRow
	idx := 0
	for gi := range p.groups {
		out = append(out, bodyRow{kind: 0, group: gi})
		for fi := range p.groups[gi].files {
			out = append(out, bodyRow{kind: 1, group: gi, file: fi})
			for range p.groups[gi].files[fi].items {
				out = append(out, bodyRow{kind: 2, group: gi, file: fi, match: idx})
				idx++
			}
		}
	}
	return out
}

func (p *Popup) rowCount() int {
	n := 0
	for _, g := range p.groups {
		n += 1 + len(g.files) + g.count
	}
	return n
}

// matchAtBodyRow resolves a body-row index to its flattened match index.
func (p *Popup) matchAtBodyRow(row int) (int, bool) {
	rows := p.rows()
	if row < 0 || row >= len(rows) || rows[row].kind != 2 {
		return 0, false
	}
	return rows[row].match, true
}

// headerRows is the fixed row count above the scrollable body (inside the
// border): title + summary line.
func (p *Popup) headerRows() int { return 2 }

// theme returns the active palette, defaulting when none was threaded in.
func (p *Popup) theme() *theme.Palette {
	if p.pal != nil {
		return p.pal
	}
	return theme.DefaultPalette()
}

// View renders the box. It is composited by the root model at Rect.
func (p *Popup) View() string {
	if !p.visible || p.width <= 0 {
		return ""
	}
	pal := p.theme()
	boxW := p.boxWidth()
	innerW := boxW - 4 // border + padding
	dim := lipgloss.NewStyle().Faint(true)

	title := lipgloss.NewStyle().Bold(true).Render("All-Projects Search")
	if p.query != "" {
		title += dim.Render("  " + ansi.Truncate(p.query, innerW-24, "…"))
	}
	lines := []string{ansi.Truncate(title, innerW, "…"), p.summaryRow(innerW)}

	rows := p.rows()
	bodyH := p.bodyHeight()
	// Keep the cursor row in the scroll window while focused.
	if p.focused {
		if cr := p.cursorBodyRow(rows); cr >= 0 {
			if cr < p.scroll {
				p.scroll = cr
			}
			if cr >= p.scroll+bodyH {
				p.scroll = cr - bodyH + 1
			}
		}
	}
	if max := len(rows) - bodyH; p.scroll > max {
		p.scroll = max
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
	end := min(p.scroll+bodyH, len(rows))
	for _, r := range rows[p.scroll:end] {
		lines = append(lines, p.renderRow(r, innerW))
	}
	if len(rows) == 0 {
		lines = append(lines, dim.Render("no matches"))
	}
	lines = append(lines, p.footerRow(innerW))

	border := pal.Border
	if p.focused {
		border = pal.BorderFocus
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(boxW - 2).
		Render(strings.Join(lines, "\n"))
}

// cursorBodyRow finds the body-row index of the selected match.
func (p *Popup) cursorBodyRow(rows []bodyRow) int {
	for i, r := range rows {
		if r.kind == 2 && r.match == p.cursor {
			return i
		}
	}
	return -1
}

// summaryRow is the header line: total count, projects searched, truncation
// and per-root errors.
func (p *Popup) summaryRow(width int) string {
	pal := p.theme()
	s := plural(p.total, "match", "matches") + " in " + plural(p.searched, "project", "projects")
	if p.truncated {
		s += " (truncated)"
	}
	row := lipgloss.NewStyle().Faint(true).Render(s)
	if n := len(p.errs); n > 0 {
		row += lipgloss.NewStyle().Foreground(pal.Error).Render("  " + plural(n, "root failed", "roots failed"))
	}
	return ansi.Truncate(row, width, "…")
}

// renderRow renders one body row.
func (p *Popup) renderRow(r bodyRow, width int) string {
	pal := p.theme()
	dim := lipgloss.NewStyle().Faint(true)
	switch r.kind {
	case 0:
		g := p.groups[r.group]
		head := lipgloss.NewStyle().Bold(true).Foreground(pal.BorderFocus).Render(g.name) +
			dim.Render("  "+g.root+"  ("+plural(g.count, "match", "matches")+")")
		if err, ok := p.errs[g.root]; ok && err != nil {
			head += lipgloss.NewStyle().Foreground(pal.Error).Render("  ✖ " + err.Error())
		}
		return ansi.Truncate(head, width, "…")
	case 1:
		g := p.groups[r.group]
		rel := g.files[r.file].path
		if rp, err := filepath.Rel(g.root, rel); err == nil {
			rel = rp
		}
		return ansi.Truncate("  "+lipgloss.NewStyle().Foreground(pal.Foreground).Render(rel), width, "…")
	default:
		g := p.groups[r.group]
		it := g.files[r.file].items[indexInFile(p, r)]
		line := "    " + dim.Render(strconv.Itoa(it.Line)+": ") + highlightMatch(it, pal)
		line = ansi.Truncate(line, width, "…")
		if p.focused && r.match == p.cursor {
			return lipgloss.NewStyle().Reverse(true).Render(ansi.Strip(line))
		}
		return line
	}
}

// indexInFile converts a row's flattened match index back to the offset
// within its file group.
func indexInFile(p *Popup, r bodyRow) int {
	idx := r.match
	for gi := 0; gi < r.group; gi++ {
		idx -= p.groups[gi].count
	}
	for fi := 0; fi < r.file; fi++ {
		idx -= len(p.groups[r.group].files[fi].items)
	}
	return idx
}

// highlightMatch renders the match line with the hit range emphasized.
func highlightMatch(it popItem, pal *theme.Palette) string {
	r := []rune(strings.TrimLeft(it.Text, " \t"))
	trim := len([]rune(it.Text)) - len(r)
	s, e := it.StartCol-trim, it.EndCol-trim
	if s < 0 {
		s = 0
	}
	if e > len(r) {
		e = len(r)
	}
	if s >= e {
		return string(r)
	}
	hl := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true)
	return string(r[:s]) + hl.Render(string(r[s:e])) + string(r[e:])
}

// footerRow spells the popup's keys out, varying with focus.
func (p *Popup) footerRow(width int) string {
	dim := lipgloss.NewStyle().Faint(true)
	if p.focused {
		return dim.Render(ansi.Truncate("enter opens (switches project if needed) — esc hides", width, "…"))
	}
	return dim.Render(ansi.Truncate("not focused — click or run Show all-projects search results", width, "…"))
}

// plural renders "1 match" / "3 matches" style counts.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

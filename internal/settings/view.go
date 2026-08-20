package settings

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/theme"
)

// The panel renders a fixed three-column grid (0460, #1295): the wireframes
// make the raster the law, so a page always looks the same and the eye learns
// one layout instead of one per page.
//
//	24ch nav · 44ch settings + value marker · rest detail = explanation + editor
const (
	// catWidth is the left category column's width.
	catWidth = 24
	// formWidth is the settings column's width when the panel is wide enough.
	formWidth = 44
	// minFormWidth is the narrowest the settings column may shrink to before
	// the grid gives up on three columns.
	minFormWidth = 28
	// minDetailWidth is the narrowest useful detail column; below it the
	// detail moves under the settings column instead of beside it.
	minDetailWidth = 24
	// sepWidth is the " │ " column separator's width.
	sepWidth = 3
	// bandDetailHeight is the detail band's height in the stacked fallback.
	bandDetailHeight = 8
)

// grid is the resolved column geometry for the current panel size.
type grid struct {
	formW, detailW int
	// side reports the detail column renders beside the settings column; when
	// false the panel is too narrow for three columns and the detail becomes a
	// band under the settings list.
	side    bool
	detailH int
	// listH is the settings column's list height.
	listH int
}

// bodyTop is the body's first panel-local y: border row, title row and the
// column-header row (#1664).
const bodyTop = 3

// chromeRows is the panel rows that are not body: two border rows, the title
// row, the column-header row (#1664) and the hint row.
const chromeRows = 5

// gridFor resolves the column geometry for the panel's size. rightW is the
// width available to everything right of the category rail.
func (m *Model) gridFor() grid {
	innerW := m.width - 2
	innerH := m.height - chromeRows
	rightW := innerW - catWidth - sepWidth
	if rightW >= minFormWidth+sepWidth+minDetailWidth {
		// The settings column takes its nominal 44ch where there is room and
		// shrinks before the detail column is dropped: three columns are worth
		// more than a wide value column.
		formW := formWidth
		if max := rightW - sepWidth - minDetailWidth; formW > max {
			formW = max
		}
		return grid{formW: formW, detailW: rightW - formW - sepWidth, side: true, detailH: innerH, listH: innerH}
	}
	// Stacked fallback: the detail keeps its editor, it just moves under the
	// list — narrow terminals lose the column, never the third column's
	// content (#1295).
	g := grid{formW: rightW, detailW: rightW, detailH: bandDetailHeight}
	if g.detailH > innerH/2 {
		g.detailH = innerH / 2
	}
	if g.detailH < 1 {
		g.detailH = 1
	}
	g.listH = innerH - g.detailH - 1 // the divider row
	if g.listH < 1 {
		g.listH = 1
	}
	return g
}

// columnRule is the vertical " │ " separator as an h-line block. A single
// " │ " string would only rule the first line — JoinHorizontal pads the rest
// with blanks — so the columns would visibly run together below row 0.
func columnRule(h int) string {
	if h < 1 {
		return " │ "
	}
	lines := make([]string, h)
	for i := range lines {
		lines[i] = " │ "
	}
	return strings.Join(lines, "\n")
}

// splitGrid splits a custom page's area into the grid's settings and detail
// columns (#1298). Pages adopting the raster get the same widths the schema
// pages have, so the eye keeps one layout across the whole panel; too narrow
// for both, the page keeps the full width and renders detail its own way.
func splitGrid(w int) (listW, detailW int, side bool) {
	if w < minFormWidth+sepWidth+minDetailWidth {
		return w, 0, false
	}
	listW = formWidth
	if maxList := w - sepWidth - minDetailWidth; listW > maxList {
		listW = maxList
	}
	return listW, w - listW - sepWidth, true
}

// theme returns the active palette, defaulting when none was threaded in.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// View renders the panel as a floating box: a rounded border around the title
// row, the three-column body and the hint row. m.width/m.height are the box's
// outer dimensions; the app centers the result above the workspace (#115).
func (m *Model) View() string {
	if !m.open || m.width < 24 || m.height < 8 {
		return ""
	}
	pal := m.theme()
	innerW := m.width - 2          // content columns inside the border (v2 sizes outer)
	inner := m.height - chromeRows // body rows under the column headers
	sep := columnRule(inner)

	m.syncEditor()
	headers := m.renderColumnHeaders(innerW)
	left := m.renderCategories(inner)
	var body string
	if page := m.customPage(); page != nil && m.filter == "" {
		// Custom pages own everything right of the rail and bring their own
		// internal layout; the grid applies to schema pages.
		right := page.View(innerW-catWidth-sepWidth, inner)
		right = lipgloss.NewStyle().MaxWidth(innerW - catWidth - sepWidth).Render(right)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
	} else {
		g := m.gridFor()
		form := m.renderForm(g.formW, g.listH)
		detail := m.renderDetailColumn(g.detailW, g.detailH)
		if g.side {
			body = lipgloss.JoinHorizontal(lipgloss.Top, left, sep, form, sep, detail)
		} else {
			// The stacked divider doubles as the detail band's focus cue
			// (#1664): accent while the band owns the keys.
			divStyle := lipgloss.NewStyle().Foreground(pal.Secondary)
			if m.focus == detailColumn {
				divStyle = lipgloss.NewStyle().Foreground(pal.BorderFocus)
			}
			div := divStyle.Render(strings.Repeat("─", g.formW))
			body = lipgloss.JoinHorizontal(lipgloss.Top, left, sep,
				strings.Join([]string{form, div, detail}, "\n"))
		}
	}

	titleText := " SETTINGS "
	if m.cat >= 0 && m.cat < len(m.pages) {
		// The current page lives in the header (#890): position at a glance.
		titleText = " SETTINGS › " + m.pages[m.cat].Title + " "
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(pal.BorderFocus).Render(titleText)
	// The write scope (0380, #794) is always-visible, clickable chrome (#885):
	// a press cycles auto → user → project like "s".
	chipStyle := lipgloss.NewStyle().Foreground(pal.Secondary)
	if m.writeScope != scopeAuto {
		chipStyle = lipgloss.NewStyle().Foreground(pal.Info).Bold(true)
	}
	chip := "[scope: " + m.scopeLabel() + "] "
	m.chipSpan = span{start: 1 + lipgloss.Width(titleText), end: 1 + lipgloss.Width(titleText) + lipgloss.Width(chip)}
	title += chipStyle.Render(chip)
	title += m.renderChangeCount(pal, lipgloss.Width(titleText)+lipgloss.Width(chip))
	title += m.renderFilter()
	hint := m.renderHint(pal)

	content := lipgloss.JoinVertical(lipgloss.Left, title, headers, body, hint)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Background(pal.Surface).
		Foreground(pal.Foreground).
		Width(m.width).
		Height(m.height).
		Render(content)
	if m.SubOpen() {
		// The open sub-panel (#883) composes centered over the panel.
		return m.renderSub(box)
	}
	return box
}

// renderColumnHeaders renders the column-header row (#1664): one caption per
// grid column, the focused one marked ▸ and accent-bold, the inactive ones
// dimmed — so which column owns the keyboard is readable at a glance, like a
// focused pane's border elsewhere.
func (m *Model) renderColumnHeaders(innerW int) string {
	pal := m.theme()
	active := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true)
	inactive := lipgloss.NewStyle().Foreground(pal.Secondary).Faint(true)
	caption := func(label string, focused bool, w int) string {
		text := "  " + label
		style := inactive
		if focused {
			text, style = "▸ "+label, active
		}
		return lipgloss.NewStyle().MaxWidth(w).Width(w).Render(style.Render(text))
	}

	out := caption("Pages", m.focus == catColumn, catWidth) + strings.Repeat(" ", sepWidth)
	rightW := innerW - catWidth - sepWidth
	if page := m.customPage(); page != nil && m.filter == "" {
		// A custom page spans the whole right side; its caption is the page.
		return out + caption(m.pages[m.cat].Title, m.focus != catColumn, rightW)
	}
	g := m.gridFor()
	if g.side {
		return out + caption("Settings", m.focus == formColumn, g.formW) +
			strings.Repeat(" ", sepWidth) + caption("Detail", m.focus == detailColumn, g.detailW)
	}
	// Stacked fallback: one caption names whichever half owns the keys.
	label := "Settings"
	if m.focus == detailColumn {
		label = "Detail"
	}
	return out + caption(label, m.focus != catColumn, g.formW)
}

// renderHint renders the bottom hint row: three context keys, not a permanent
// nine-key legend (#1295). The full list lives behind "?" — the cheatsheet
// overlay — so the footer can always say the three things that matter here.
// The segments stay clickable (#885).
func (m *Model) renderHint(pal *theme.Palette) string {
	m.hintHits = nil
	style := lipgloss.NewStyle().Foreground(pal.Secondary)
	switch {
	case m.SubOpen():
		return style.Render(" esc back · click a button")
	case m.filtering:
		return style.Render(" type to filter · enter keep · esc clear")
	}
	var segs []hintSeg
	switch {
	case m.filter != "" && m.focus == formColumn:
		// Search owns the grid (#1297): a value is settable straight from a
		// result, and tab leaves for the owning page.
		segs = []hintSeg{{"enter set here", "edit"}, {"tab open page", "openpage"}}
	case m.focus == catColumn:
		segs = []hintSeg{{"enter settings", "edit"}, {"/ search", "filter"}}
	case m.focus == detailColumn:
		segs = m.editorHint()
	default:
		segs = []hintSeg{{"enter edit", "edit"}, {"r reset", "reset"}}
	}
	if m.Dirty() {
		// With edits pending, applying them is what matters most here.
		segs = append(segs[:1:1], hintSeg{"ctrl+s apply", "apply"})
	}
	segs = append(segs, hintSeg{"? all keys", "help"})

	x := 1 // border column 0
	var out string
	for i, sg := range segs {
		if i > 0 {
			out += " · "
			x += 3
		} else {
			out += " "
			x++
		}
		w := lipgloss.Width(sg.text)
		if sg.action != "" {
			m.hintHits = append(m.hintHits, hintAction{start: x, end: x + w, action: sg.action})
		}
		out += sg.text
		x += w
	}
	return style.Render(out)
}

// renderChangeCount renders the staged-apply counter in the header (#1296) and
// records its clickable span: "● n changed · ctrl+s apply" while edits are
// pending, nothing at all when there are none — a quiet header means a clean
// panel.
func (m *Model) renderChangeCount(pal *theme.Palette, x int) string {
	m.countSpan = span{}
	if !m.Dirty() {
		return ""
	}
	text := "● " + plural(len(m.changes), "change") + " · ctrl+s apply "
	m.countSpan = span{start: 1 + x, end: 1 + x + lipgloss.Width(text)}
	return lipgloss.NewStyle().Foreground(pal.Warning).Bold(true).Render(text)
}

// markerWidth is the selection marker's width in cells: the glyph plus the
// space separating it from the label. Unselected rows pay the same width as
// blanks, so a moving selection never shifts the text column (#1689).
const markerWidth = 2

// rowMarker renders the two-cell selection marker used by every settings list
// (#1689), mirroring Search Everywhere / Recent Files (#1532): the accent "❯"
// on the focused column's selected row, a muted one on an unfocused column's,
// blanks elsewhere. bg carries the row's own background so the marker sits on
// the selection band instead of punching a hole in it.
func rowMarker(pal *theme.Palette, selected, focused bool, bg lipgloss.Style) string {
	if !selected {
		return bg.Render("  ")
	}
	colour := pal.Border
	if focused {
		colour = pal.Accent
	}
	return bg.Foreground(colour).Bold(true).Render("❯ ")
}

// markerBG picks the marker's backdrop: the selection band on a selected row,
// the plain background otherwise.
func markerBG(selected bool, sel lipgloss.Style) lipgloss.Style {
	if selected {
		return sel
	}
	return lipgloss.NewStyle()
}

// hintSeg is one footer segment: its text and the action a click runs.
type hintSeg struct{ text, action string }

// editorHint names the two keys that matter for the focused editor.
func (m *Model) editorHint() []hintSeg {
	switch m.editor.(type) {
	case *enumEditor:
		return []hintSeg{{"↑↓ select", ""}, {"enter apply", ""}}
	case *listEditor:
		return []hintSeg{{"enter edit", ""}, {"d remove", ""}}
	case *intEditor:
		return []hintSeg{{"‹› value", ""}, {"enter apply", ""}}
	case *boolEditor:
		return []hintSeg{{"space toggle", ""}, {"esc back", ""}}
	case *chordEditor:
		return []hintSeg{{"enter record", ""}, {"esc back", ""}}
	default:
		return []hintSeg{{"enter apply", ""}, {"esc cancel", ""}}
	}
}

// renderFilter shows the live filter input on the title row.
func (m *Model) renderFilter() string {
	if m.filter == "" && !m.filtering {
		return ""
	}
	pal := m.theme()
	text := " ⌕ " + m.filter
	if m.filtering {
		text = " ⌕ " + filterView(m.filter, m.filterCur)
	}
	out := lipgloss.NewStyle().Foreground(pal.Info).Render(text)
	if m.filter != "" {
		// The header answers "how much did that match, and where" (#1297).
		out += lipgloss.NewStyle().Foreground(pal.Secondary).Render(" · " + m.hitSummary())
	}
	return out
}

// renderCategories renders the page list; filtering dims it (results span all
// pages then). The list scrolls so the selected page is always visible; the
// unfocused column keeps a dimmed selection background so the focused column
// is unambiguous.
func (m *Model) renderCategories(h int) string {
	if m.filter != "" {
		return m.renderHitPages(h)
	}
	pal := m.theme()
	base := lipgloss.NewStyle().Width(catWidth)
	// The marker owns the first two cells, so the labels are that much
	// narrower and the column still totals catWidth.
	lbl := lipgloss.NewStyle().Width(catWidth - markerWidth)
	sel := lbl.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	inactiveSel := lbl.Background(pal.Selection).Foreground(pal.SelectionText).Faint(true)
	selBG := lipgloss.NewStyle().Background(pal.Selection)
	header := base.Foreground(pal.Secondary).Faint(true).Bold(true)
	hover := lbl.Underline(true)

	rows := m.railRows()
	selRow := m.railRowOf(m.cat)
	if m.followCat {
		m.catOff = follow(m.catOff, selRow, selRow, len(rows), h)
		m.followCat = false
	}
	m.catOff = clamp(m.catOff, 0, maxOff(len(rows), h))
	lines := make([]string, 0, h)
	for i := m.catOff; i < len(rows) && len(lines) < h; i++ {
		r := rows[i]
		if r.header != "" {
			lines = append(lines, header.Render(" "+r.header))
			continue
		}
		p := m.pages[r.page]
		label := p.Title
		if n := m.changedOnPage(r.page); n > 0 {
			// The rail says which pages carry staged edits (#1296), so a
			// batch spanning pages is visible from anywhere.
			label += " ●" + strconv.Itoa(n)
		}
		selected := r.page == m.cat
		mark := rowMarker(pal, selected, m.focus == catColumn, markerBG(selected, selBG))
		switch {
		case selected && m.focus == catColumn:
			lines = append(lines, mark+sel.Render(label))
		case selected:
			lines = append(lines, mark+inactiveSel.Render(label))
		case r.page == m.hoverCat:
			lines = append(lines, mark+hover.Render(label))
		default:
			lines = append(lines, mark+lbl.Render(label))
		}
	}
	for len(lines) < h {
		lines = append(lines, base.Render(""))
	}
	// Scroll indicators (#890): the rail says when more categories exist
	// above/below the window.
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	if m.catOff > 0 && len(lines) > 0 {
		lines[0] = base.Render(dim.Render(" ▲ more"))
	}
	if m.catOff+h < len(rows) && len(lines) > 0 {
		lines[h-1] = base.Render(dim.Render(" ▼ more"))
	}
	return strings.Join(lines[:h], "\n")
}

// renderHitPages is the nav column in search mode (#1297): the pages carrying
// matches, with their counts, instead of the full category list — the query's
// shape at a glance, and a way to jump the match list page by page.
func (m *Model) renderHitPages(h int) string {
	pal := m.theme()
	base := lipgloss.NewStyle().Width(catWidth)
	lbl := lipgloss.NewStyle().Width(catWidth - markerWidth)
	sel := lbl.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	inactiveSel := lbl.Background(pal.Selection).Foreground(pal.SelectionText).Faint(true)
	selBG := lipgloss.NewStyle().Background(pal.Selection)
	dim := base.Foreground(pal.Secondary)

	pages := m.hitPages()
	lines := []string{dim.Render(" pages with hits")}
	m.hitSel = clamp(m.hitSel, 0, len(pages)-1)
	m.catOff = clamp(m.catOff, 0, maxOff(len(pages), h-1))
	for i := m.catOff; i < len(pages) && len(lines) < h; i++ {
		p := pages[i]
		label := m.pages[p.page].Title + " " + strconv.Itoa(p.count)
		selected := i == m.hitSel
		mark := rowMarker(pal, selected, m.focus == catColumn, markerBG(selected, selBG))
		switch {
		case selected && m.focus == catColumn:
			lines = append(lines, mark+sel.Render(label))
		case selected:
			lines = append(lines, mark+inactiveSel.Render(label))
		default:
			lines = append(lines, mark+lbl.Render(label))
		}
	}
	for len(lines) < h {
		lines = append(lines, base.Render(""))
	}
	return strings.Join(lines[:h], "\n")
}

// follow adjusts a scroll offset so the [selStart, selEnd] line range stays
// visible in a window of h lines over n total lines.
func follow(off, selStart, selEnd, n, h int) int {
	if h <= 0 {
		return 0
	}
	if selEnd >= off+h {
		off = selEnd - h + 1
	}
	if selStart < off {
		off = selStart
	}
	if off > n-h {
		off = n - h
	}
	if off < 0 {
		off = 0
	}
	return off
}

// pinFooter lays out a scrolling list with a footer pinned to the bottom of an
// h-line window (#537): the list is windowed with follow so the [selStart,
// selEnd] lines stay visible, padded to a constant height, and the footer
// renders below it — so a selection move can never shift the list rows. When
// the window is too short for the footer, the list wins.
func pinFooter(list, footer []string, selStart, selEnd, h int, off *int) string {
	return pinFooterLazy(len(list), func(i int) string { return list[i] }, footer, selStart, selEnd, h, off)
}

// pinFooterLazy is pinFooter for lists whose rows map 1:1 to lines and are
// rendered on demand (#1396): the window is resolved first and render is only
// called for the visible rows, so a long list (the keymap table) costs the
// window per frame, not the list.
func pinFooterLazy(n int, render func(i int) string, footer []string, selStart, selEnd, h int, off *int) string {
	if h < 1 {
		return ""
	}
	listH := h - len(footer)
	if listH < 1 {
		footer, listH = nil, h
	}
	*off = follow(*off, selStart, selEnd, n, listH)
	end := *off + listH
	if end > n {
		end = n
	}
	out := make([]string, 0, listH+len(footer))
	for i := *off; i < end; i++ {
		out = append(out, render(i))
	}
	if len(footer) > 0 {
		for len(out) < listH {
			out = append(out, "")
		}
		out = append(out, footer...)
	}
	return strings.Join(out, "\n")
}

// footerLine couples a pinned-footer text with its style before wrapping.
type footerLine struct {
	text  string
	style lipgloss.Style
}

// wrapFooter word-wraps each footer line to width w (wrapping the raw text,
// then styling every resulting line) and pads/clamps the result to exactly
// want lines (#553): custom-page hints stay readable in a narrow column
// instead of clipping, and the constant count keeps pinFooter's
// no-jumpiness invariant. Overflow beyond want lines is marked with "…".
func wrapFooter(lines []footerLine, w, want int) []string {
	if w < 2 {
		w = 2
	}
	out := make([]string, 0, want)
	for _, l := range lines {
		if l.text == "" {
			if len(out) < want {
				out = append(out, "")
			}
			continue
		}
		for _, part := range strings.Split(ansi.Wordwrap(l.text, w-1, ""), "\n") {
			if len(out) == want {
				out[want-1] += "…"
				return out
			}
			out = append(out, l.style.Render(part))
		}
	}
	for len(out) < want {
		out = append(out, "")
	}
	return out
}

// padTo pads lines with blanks and clamps them to exactly h entries.
func padTo(lines []string, h int) []string {
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

// renderForm renders the settings column: one line per entry, "Title … value
// marker". Nothing expands inline any more (0460, #1295) — the editor and the
// help both live in the detail column, so the rows map 1:1 to lines and a
// selection move can never shift what is under the pointer.
func (m *Model) renderForm(w, h int) string {
	pal := m.theme()
	// The column has a fixed edge: lines are clipped *and* padded to w, so a
	// short list (a filter result) cannot shrink the column and shift the
	// detail column sideways.
	clip := lipgloss.NewStyle().MaxWidth(w).Width(w)
	rows := m.rows()
	if len(rows) == 0 {
		empty := clip.Render(lipgloss.NewStyle().Foreground(pal.Secondary).Render(" no matching settings"))
		return strings.Join(padTo([]string{empty}, h), "\n")
	}
	lines := make([]string, 0, len(rows)+1)
	for i, r := range rows {
		lines = append(lines, clip.Render(m.renderEntry(r, i == m.sel, i == m.hoverRow, w)))
	}
	if m.filter != "" {
		if note := m.customPagesNote(); note != "" {
			lines = append(lines, clip.Render(
				lipgloss.NewStyle().Foreground(pal.Secondary).Faint(true).Render(note)))
		}
	}
	if m.followForm {
		m.formOff = follow(m.formOff, m.sel, m.sel, len(lines), h)
		m.followForm = false
	}
	m.formOff = clamp(m.formOff, 0, maxOff(len(lines), h))
	end := m.formOff + h
	if end > len(lines) {
		end = len(lines)
	}
	out := append([]string{}, lines[m.formOff:end]...)
	// Scroll indicators (#890).
	ind := lipgloss.NewStyle().Foreground(pal.Secondary)
	if m.formOff > 0 && len(out) > 0 {
		out[0] = clip.Render(ind.Render(" ▲ more"))
	}
	if end < len(lines) && len(out) > 0 {
		out[len(out)-1] = clip.Render(ind.Render(" ▼ more"))
	}
	return strings.Join(padTo(out, h), "\n")
}

// renderDetailColumn renders the third column (#1295). It is never blank: with
// the rail focused — or with nothing selectable — it explains the category, so
// browsing pages already teaches what they are for; otherwise it carries the
// selected entry's documentation, its provenance and its editor.
func (m *Model) renderDetailColumn(w, h int) string {
	if h < 1 || w < 4 {
		return ""
	}
	pal := m.theme()
	clip := lipgloss.NewStyle().MaxWidth(w)
	title := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true)
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)

	wrap := func(text string, style lipgloss.Style) []string {
		var out []string
		for _, l := range strings.Split(ansi.Wordwrap(text, w-2, ""), "\n") {
			out = append(out, clip.Render(style.Render(" "+l)))
		}
		return out
	}

	// Until an editor body is drawn this frame there is nothing clickable in
	// the column (#1664): -1 tells clickDetail the head spans everything.
	m.detailBodyTop = -1

	r, ok := m.current()
	if ok && r.kind != rowEntry && m.focus != catColumn {
		// A filter jump row (#886): say where enter goes rather than
		// describing the page the cursor happens to sit on.
		lines := []string{clip.Render(title.Render(" " + r.label))}
		if r.kind == rowPage {
			lines = append(lines, wrap("A settings page. Enter opens it.", dim)...)
		} else {
			lines = append(lines, wrap("Enter opens the page positioned on this item.", dim)...)
		}
		return strings.Join(padTo(lines, h), "\n")
	}
	if !ok || r.kind != rowEntry || m.focus == catColumn {
		return strings.Join(padTo(m.renderPageDetail(w, h, wrap, title, dim), h), "\n")
	}

	e := r.entry
	head := []string{clip.Render(title.Render(" " + e.Title))}
	head = append(head, wrap(e.Description, dim)...)
	meta := e.Key + " · " + typeName(e.Type)
	if def, hasDefault := config.Defaults()[e.Key]; hasDefault {
		meta += " · default: " + def
	}
	head = append(head, wrap(meta, dim)...)
	head = append(head, clip.Render(dim.Render(" "+strings.Repeat("─", maxInt(w-2, 1)))))

	foot := m.detailFoot(e, w, clip, dim, pal)

	m.detailBodyTop = len(head) // the editor body's first line (#1325)
	body := []string{}
	if m.editor != nil {
		bodyH := h - len(head) - len(foot)
		if bodyH > 0 {
			body = m.editor.View(w, bodyH)
			if len(body) > bodyH {
				body = body[:bodyH]
			}
		}
	}
	out := append(append([]string{}, head...), body...)
	for len(out) < h-len(foot) {
		out = append(out, "")
	}
	out = append(out, foot...)
	return strings.Join(padTo(out, h), "\n")
}

// detailFoot renders the detail column's pinned bottom band: the write
// feedback (clamp notice, validation and write errors, #889/#891) and where
// the value comes from and where an edit would go.
func (m *Model) detailFoot(e Entry, w int, clip, dim lipgloss.Style, pal *theme.Palette) []string {
	var out []string
	switch {
	case m.writeErr != "":
		out = append(out, clip.Render(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+m.writeErr)))
	case m.notice != "":
		out = append(out, clip.Render(lipgloss.NewStyle().Foreground(pal.Info).Render(" "+m.notice)))
	default:
		out = append(out, "")
	}
	if c, staged := m.change(e.Key); staged {
		// A staged row says where it came from and how to take it back
		// (#1296) — the counter alone would not tell you what changed.
		arrow := " ● " + c.old + " → " + c.shown
		if c.reset {
			arrow = " ● " + c.old + " → default"
		}
		out = append(out, clip.Render(lipgloss.NewStyle().Foreground(pal.Warning).Render(arrow)))
	}
	origin := config.Origin(m.opts, e.Key)
	scope := "user"
	if m.scopeFor(e) == config.ProjectScope {
		scope = "project"
	}
	out = append(out, clip.Render(dim.Render(" set in "+origin+" · writes to "+scope)))
	return out
}

// renderPageDetail is the detail column's no-selection state: the category's
// own description plus what it contains.
func (m *Model) renderPageDetail(w, h int, wrap func(string, lipgloss.Style) []string, title, dim lipgloss.Style) []string {
	clip := lipgloss.NewStyle().MaxWidth(w)
	if m.cat < 0 || m.cat >= len(m.pages) {
		return nil
	}
	p := m.pages[m.cat]
	out := []string{clip.Render(title.Render(" " + p.Title))}
	if p.Description != "" {
		out = append(out, wrap(p.Description, dim)...)
	}
	out = append(out, "")
	switch {
	case p.Custom != nil:
		out = append(out, clip.Render(dim.Render(" enter opens this page")))
	case len(p.Entries) > 0:
		out = append(out, clip.Render(dim.Render(" "+strconv.Itoa(len(p.Entries))+" settings")))
		out = append(out, clip.Render(dim.Render(" enter to browse them")))
	}
	return out
}

// affordanceValue renders a value with its type's marker (#1295): the suffix
// says what enter will open — ▸ a list, ‹› a stepper, ◉ a toggle, ⌨ a capture,
// ≡ a multi-value list, ✎ free text — so beginners do not have to guess and
// power users read it peripherally.
func affordanceValue(e Entry, val string) string {
	if e.Type == List || e.Type == IntList {
		val = strings.Trim(val, "[]")
		val = strings.Join(splitList(val), ", ")
		if val == "" {
			val = "(empty)"
		}
	}
	if e.Type == Chord && val == "" {
		val = "(unbound)"
	}
	return val + " " + marker(e.Type)
}

// customPagesNote names the custom pages the filter cannot search (the ones
// not yet exporting SearchItems, #886).
func (m *Model) customPagesNote() string {
	var names []string
	for _, p := range m.pages {
		if p.Custom == nil {
			continue
		}
		if _, ok := p.Custom.(Searchable); !ok {
			names = append(names, p.Title)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "   (not searched: " + strings.Join(names, ", ") + ")"
}

// highlightMatch marks the first case-insensitive occurrence of needle in
// text. Styling only the match keeps the row readable: the eye lands on why
// the row is in the list.
func highlightMatch(text, needle string, pal *theme.Palette) string {
	if needle == "" {
		return text
	}
	i := strings.Index(strings.ToLower(text), strings.ToLower(needle))
	if i < 0 {
		return text
	}
	hit := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	return text[:i] + hit.Render(text[i:i+len(needle)]) + text[i+len(needle):]
}

// renderEntry renders one settings-column row: "Title … value marker". The
// origin badge moved into the detail column (#1295) — the marker earns that
// space, and provenance is a detail, not a scanning cue.
func (m *Model) renderEntry(r row, selected, hovered bool, w int) string {
	pal := m.theme()
	selBG := lipgloss.NewStyle().Background(pal.Selection)
	if r.kind != rowEntry {
		// A filter jump row (#886): a page or a custom-page item.
		label := "→ " + highlightMatch(r.label, m.filter, pal)
		if r.kind == rowPage {
			label += "  (page)"
		}
		mark := rowMarker(pal, selected, m.focus == formColumn, markerBG(selected, selBG))
		style := lipgloss.NewStyle().Foreground(pal.Info)
		switch {
		case selected && m.focus == formColumn:
			style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		case selected:
			style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Faint(true)
		case hovered:
			style = style.Underline(true)
		}
		return mark + style.Render(label)
	}
	e := r.entry

	// The marker owns the row's first two cells (#1689); the rest of the row
	// lays out in what is left, so the column still totals w.
	avail := maxInt(w-markerWidth, 1)
	title := e.Title
	if m.filter != "" {
		// Search mode marks what matched (#1297), so a hit list is readable
		// without re-reading the query.
		title = m.pages[r.page].Title + " › " + highlightMatch(e.Title, m.filter, pal)
	}
	right := affordanceValue(e, m.value(e.Key)) + " "
	// The value yields before the title does: a long list still shows its
	// marker, and the row never overflows the fixed column.
	if over := lipgloss.Width(title) + lipgloss.Width(right) + 1 - avail; over > 0 {
		right = ansi.Truncate(right, maxInt(lipgloss.Width(right)-over, 0), "…")
	}
	gap := avail - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := title + strings.Repeat(" ", gap) + right

	style := lipgloss.NewStyle()
	switch {
	case selected && m.focus != catColumn:
		style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	case selected:
		// Unfocused column: keep the selection visible but dimmed, so the
		// vivid selection always marks the focused column.
		style = style.Background(pal.Selection).Foreground(pal.SelectionText).Faint(true)
	case hovered:
		style = style.Underline(true) // pointer affordance (#885)
	case config.Origin(m.opts, e.Key) == "default":
		style = style.Foreground(pal.Foreground)
	default:
		style = style.Foreground(pal.Info) // overridden values stand out
	}
	mark := rowMarker(pal, selected, m.focus != catColumn, markerBG(selected, selBG))
	return mark + style.Render(line)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

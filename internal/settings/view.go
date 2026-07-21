package settings

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/theme"
)

// catWidth is the left category column's width.
const catWidth = 22

// theme returns the active palette, defaulting when none was threaded in.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// View renders the panel as a floating box: a rounded border around the title
// row, the two-column body and the hint row. m.width/m.height are the box's
// outer dimensions; the app centers the result above the workspace (#115).
func (m *Model) View() string {
	if !m.open || m.width < 24 || m.height < 8 {
		return ""
	}
	pal := m.theme()
	innerW := m.width - 2 // content columns inside the border (v2 sizes outer)
	inner := m.height - 4 // border rows + title row + hint row

	left := m.renderCategories(inner)
	rightW := innerW - catWidth - 3
	var right string
	if page := m.customPage(); page != nil && m.filter == "" {
		right = page.View(rightW, inner)
	} else {
		right = m.renderForm(rightW, inner)
	}
	right = lipgloss.NewStyle().MaxWidth(rightW).Render(right)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " │ ", right)

	title := lipgloss.NewStyle().Bold(true).Foreground(pal.BorderFocus).Render(" SETTINGS ")
	// The write scope (0380, #794) is always-visible, clickable chrome (#885):
	// a press cycles auto → user → project like "s".
	chipStyle := lipgloss.NewStyle().Foreground(pal.Secondary)
	if m.writeScope != scopeAuto {
		chipStyle = lipgloss.NewStyle().Foreground(pal.Info).Bold(true)
	}
	chip := "[scope: " + m.scopeLabel() + "] "
	m.chipSpan = span{start: 1 + lipgloss.Width(" SETTINGS "), end: 1 + lipgloss.Width(" SETTINGS ") + lipgloss.Width(chip)}
	title += chipStyle.Render(chip)
	title += m.renderFilter()
	hint := m.renderHint(pal)

	content := lipgloss.JoinVertical(lipgloss.Left, title, body, hint)
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

// renderHint renders the bottom hint row and records its clickable key
// segments (#885): a press on "r reset" resets, on "s scope" cycles, and so
// on — the hints are buttons now, not just documentation.
func (m *Model) renderHint(pal *theme.Palette) string {
	m.hintHits = nil
	style := lipgloss.NewStyle().Foreground(pal.Secondary)
	switch {
	case m.SubOpen():
		return style.Render(" esc back · click a button")
	case m.picking:
		return style.Render(" ↑↓ choose · enter apply · esc cancel")
	}
	if r, ok := m.current(); ok && m.editing && r.entry.Type == Path {
		return style.Render(" tab complete path · enter apply · esc cancel")
	}
	type seg struct{ text, action string }
	segs := []seg{
		{" ↑↓/jk navigate · ←→ column · ", ""},
		{"enter edit", "edit"},
		{" · ", ""},
		{"r reset", "reset"},
		{" · ", ""},
		{"s scope", "scope"},
		{" · ", ""},
		{"/ filter", "filter"},
		{" · ", ""},
		{"esc", "close"},
	}
	x := 1 // border column 0
	var out string
	for _, sg := range segs {
		w := lipgloss.Width(sg.text)
		if sg.action != "" {
			m.hintHits = append(m.hintHits, hintAction{start: x, end: x + w, action: sg.action})
		}
		out += sg.text
		x += w
	}
	return style.Render(out)
}

// renderFilter shows the live filter input on the title row.
func (m *Model) renderFilter() string {
	if m.filter == "" && !m.filtering {
		return ""
	}
	pal := m.theme()
	text := " /" + m.filter
	if m.filtering {
		text += "▌"
	}
	return lipgloss.NewStyle().Foreground(pal.Info).Render(text)
}

// renderCategories renders the page list; filtering dims it (results span all
// pages then). The list scrolls so the selected page is always visible; the
// unfocused column keeps a dimmed selection background so the focused column
// is unambiguous.
func (m *Model) renderCategories(h int) string {
	pal := m.theme()
	base := lipgloss.NewStyle().Width(catWidth)
	sel := base.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	inactiveSel := base.Background(pal.Selection).Foreground(pal.SelectionText).Faint(true)

	if m.followCat {
		m.catOff = follow(m.catOff, m.cat, m.cat, len(m.pages), h)
		m.followCat = false
	}
	m.catOff = clamp(m.catOff, 0, maxOff(len(m.pages), h))
	hover := base.Underline(true)
	lines := make([]string, 0, h)
	for i := m.catOff; i < len(m.pages) && len(lines) < h; i++ {
		p := m.pages[i]
		label := " " + p.Title
		switch {
		case i == m.cat && m.focus == catColumn:
			lines = append(lines, sel.Render(label))
		case i == m.cat:
			lines = append(lines, inactiveSel.Render(label))
		case i == m.hoverCat:
			lines = append(lines, hover.Render(label))
		default:
			lines = append(lines, base.Render(label))
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
	if h < 1 {
		return ""
	}
	listH := h - len(footer)
	if listH < 1 {
		footer, listH = nil, h
	}
	*off = follow(*off, selStart, selEnd, len(list), listH)
	end := *off + listH
	if end > len(list) {
		end = len(list)
	}
	out := append([]string{}, list[*off:end]...)
	if len(footer) > 0 {
		for len(out) < listH {
			out = append(out, "")
		}
		out = append(out, footer...)
	}
	return strings.Join(out, "\n")
}

// detailLines is the pinned description footer's constant height (#549): the
// help text word-wraps over this many lines instead of clipping at one.
const detailLines = 2

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

// renderForm renders the visible entries with value and layer badge. The
// selected entry's description lives in a footer pinned to the bottom of the
// column — never inline — so moving the selection cannot shift the rows below
// it (#535); it wraps over detailLines lines (#549). Only the enum picker
// still expands inline (an explicit action).
func (m *Model) renderForm(w, h int) string {
	pal := m.theme()
	rows := m.rows()
	if len(rows) == 0 {
		return lipgloss.NewStyle().Foreground(pal.Secondary).Render("no matching settings")
	}
	clip := lipgloss.NewStyle().MaxWidth(w)
	listH := h - detailLines // the last lines are the pinned detail footer
	if listH < 1 {
		listH = 1
	}
	var lines []string
	selStart, selEnd := 0, 0
	for i, r := range rows {
		if i == m.sel {
			selStart = len(lines)
		}
		lines = append(lines, clip.Render(m.renderEntry(r, i == m.sel, i == m.hoverRow, w)))
		if i == m.sel {
			if m.picking {
				lines = append(lines, m.renderPicker(r.entry, clip)...)
			}
			if m.editing && r.entry.Type == Path {
				sug := lipgloss.NewStyle().Foreground(pal.Secondary)
				for _, s := range m.suggest.lines() {
					lines = append(lines, clip.Render(sug.Render(s)))
				}
			}
			selEnd = len(lines) - 1
		}
	}
	if m.filter != "" {
		if note := m.customPagesNote(); note != "" {
			lines = append(lines, clip.Render(
				lipgloss.NewStyle().Foreground(pal.Secondary).Faint(true).Render(note)))
		}
	}
	if m.followForm {
		m.formOff = follow(m.formOff, selStart, selEnd, len(lines), listH)
		m.followForm = false
	}
	m.formOff = clamp(m.formOff, 0, maxOff(len(lines), listH))
	end := m.formOff + listH
	if end > len(lines) {
		end = len(lines)
	}
	out := lines[m.formOff:end]
	if h > detailLines {
		for len(out) < listH {
			out = append(out, "")
		}
		for _, d := range m.renderDetail(w) {
			out = append(out, clip.Render(d))
		}
	}
	return strings.Join(out, "\n")
}

// renderDetail renders the pinned footer (#535): the selected entry's
// description and key, word-wrapped over a constant detailLines lines (#549)
// — long help stays readable instead of clipping at the column edge. A
// validation error takes the first line; the wrapped description continues
// below it. The result always has exactly detailLines entries so the footer
// height never shifts the list.
func (m *Model) renderDetail(w int) []string {
	out := make([]string, 0, detailLines)
	r, ok := m.current()
	if ok {
		pal := m.theme()
		style := lipgloss.NewStyle().Foreground(pal.Secondary)
		if m.invalid != "" {
			out = append(out, lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ "+m.invalid))
		}
		text := r.entry.Description + "  (" + r.entry.Key + ")"
		for _, line := range strings.Split(ansi.Wordwrap(text, w-1, ""), "\n") {
			if len(out) == detailLines {
				// Even the wrapped footer overflows: mark the cut (the
				// clip style trims the ellipsis back into the column).
				out[detailLines-1] += "…"
				break
			}
			out = append(out, style.Render(" "+line))
		}
	}
	for len(out) < detailLines {
		out = append(out, "")
	}
	return out
}

// renderPicker renders the open enum dropdown under the selected row.
func (m *Model) renderPicker(e Entry, clip lipgloss.Style) []string {
	pal := m.theme()
	base := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	cur := value(e.Key)
	out := make([]string, 0, len(e.Options))
	for i, o := range e.Options {
		line := "     " + o
		if i == m.pickIdx {
			line = "   ▸ " + o
		}
		if o == cur {
			line += " ●"
		}
		style := base
		if i == m.pickIdx {
			style = sel
		}
		out = append(out, clip.Render(style.Render(line)))
	}
	return out
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

// renderEntry renders one form row: "Title  [page]  value  @layer".
func (m *Model) renderEntry(r row, selected, hovered bool, w int) string {
	pal := m.theme()
	if r.kind != rowEntry {
		// A filter jump row (#886): a page or a custom-page item.
		label := " → " + r.label
		if r.kind == rowPage {
			label = " → " + r.label + "  (page)"
		}
		style := lipgloss.NewStyle().Foreground(pal.Info)
		switch {
		case selected && m.focus == formColumn:
			style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
		case selected:
			style = lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Faint(true)
		case hovered:
			style = style.Underline(true)
		}
		return style.Render(label)
	}
	e := r.entry

	val := value(e.Key)
	if selected && m.editing {
		if e.Type == Chord {
			val = "press a key…"
		} else {
			val = m.input + "▌"
		}
	}
	origin := config.Origin(m.opts, e.Key)

	title := " " + e.Title
	if m.filter != "" {
		title = " " + m.pages[r.page].Title + " › " + e.Title
	}
	right := val + "  @" + origin + " "
	gap := w - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := title + strings.Repeat(" ", gap) + right

	style := lipgloss.NewStyle()
	switch {
	case selected && m.focus == formColumn:
		style = style.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	case selected:
		// Unfocused column: keep the selection visible but dimmed, so the
		// vivid selection always marks the focused column.
		style = style.Background(pal.Selection).Foreground(pal.SelectionText).Faint(true)
	case hovered:
		style = style.Underline(true) // pointer affordance (#885)
	case origin == "default":
		style = style.Foreground(pal.Foreground)
	default:
		style = style.Foreground(pal.Info) // overridden values stand out
	}
	return style.Render(line)
}

package settings

import (
	"strings"

	"charm.land/lipgloss/v2"

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

	title := lipgloss.NewStyle().Bold(true).Foreground(pal.BorderFocus).Render(" SETTINGS ") + m.renderFilter()
	hintText := " ↑↓/jk navigate · ←→/tab column · enter edit · r reset · / filter · esc close"
	if m.picking {
		hintText = " ↑↓ choose · enter apply · esc cancel"
	}
	if r, ok := m.current(); ok && m.editing && r.entry.Type == Path {
		hintText = " tab complete path · enter apply · esc cancel"
	}
	hint := lipgloss.NewStyle().Foreground(pal.Secondary).Render(hintText)

	content := lipgloss.JoinVertical(lipgloss.Left, title, body, hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Background(pal.Surface).
		Foreground(pal.Foreground).
		Width(m.width).
		Height(m.height).
		Render(content)
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
	dim := base.Foreground(pal.Secondary).Faint(true)

	m.catOff = follow(m.catOff, m.cat, m.cat, len(m.pages), h)
	lines := make([]string, 0, h)
	for i := m.catOff; i < len(m.pages) && len(lines) < h; i++ {
		p := m.pages[i]
		label := " " + p.Title
		switch {
		case m.filter != "":
			lines = append(lines, dim.Render(label))
		case i == m.cat && m.focus == catColumn:
			lines = append(lines, sel.Render(label))
		case i == m.cat:
			lines = append(lines, inactiveSel.Render(label))
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

// renderForm renders the visible entries with value and layer badge. The
// selected entry's description lives in a footer pinned to the bottom of the
// column — never inline — so moving the selection cannot shift the rows below
// it (#535). Only the enum picker still expands inline (an explicit action).
func (m *Model) renderForm(w, h int) string {
	pal := m.theme()
	rows := m.rows()
	if len(rows) == 0 {
		return lipgloss.NewStyle().Foreground(pal.Secondary).Render("no matching settings")
	}
	clip := lipgloss.NewStyle().MaxWidth(w)
	listH := h - 1 // the last line is the pinned detail footer
	if listH < 1 {
		listH = 1
	}
	var lines []string
	selStart, selEnd := 0, 0
	for i, r := range rows {
		if i == m.sel {
			selStart = len(lines)
		}
		lines = append(lines, clip.Render(m.renderEntry(r, i == m.sel, w)))
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
	m.formOff = follow(m.formOff, selStart, selEnd, len(lines), listH)
	end := m.formOff + listH
	if end > len(lines) {
		end = len(lines)
	}
	out := lines[m.formOff:end]
	if h > 1 {
		for len(out) < listH {
			out = append(out, "")
		}
		out = append(out, clip.Render(m.renderDetail()))
	}
	return strings.Join(out, "\n")
}

// renderDetail renders the pinned footer line: the selected entry's
// description and key, or the current validation error.
func (m *Model) renderDetail() string {
	r, ok := m.current()
	if !ok {
		return ""
	}
	pal := m.theme()
	style := lipgloss.NewStyle().Foreground(pal.Secondary)
	text := " " + r.entry.Description + "  (" + r.entry.Key + ")"
	if m.invalid != "" {
		text = " ✗ " + m.invalid
		style = style.Foreground(pal.Error)
	}
	return style.Render(text)
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

// customPagesNote names the custom pages the filter cannot search.
func (m *Model) customPagesNote() string {
	var names []string
	for _, p := range m.pages {
		if p.Custom != nil {
			names = append(names, p.Title)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "   (not searched: " + strings.Join(names, ", ") + ")"
}

// renderEntry renders one form row: "Title  [page]  value  @layer".
func (m *Model) renderEntry(r row, selected bool, w int) string {
	pal := m.theme()
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
	case origin == "default":
		style = style.Foreground(pal.Foreground)
	default:
		style = style.Foreground(pal.Info) // overridden values stand out
	}
	return style.Render(line)
}

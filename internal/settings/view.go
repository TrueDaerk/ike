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
	hint := lipgloss.NewStyle().Foreground(pal.Secondary).
		Render(" ↑↓/jk navigate · tab column · enter edit · r reset · / filter · esc close")

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
// pages then).
func (m *Model) renderCategories(h int) string {
	pal := m.theme()
	base := lipgloss.NewStyle().Width(catWidth)
	sel := base.Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	dim := base.Foreground(pal.Secondary).Faint(true)

	lines := make([]string, 0, len(m.pages))
	for i, p := range m.pages {
		label := " " + p.Title
		switch {
		case m.filter != "":
			lines = append(lines, dim.Render(label))
		case i == m.cat && m.focus == catColumn:
			lines = append(lines, sel.Render(label))
		case i == m.cat:
			lines = append(lines, base.Bold(true).Render(label))
		default:
			lines = append(lines, base.Render(label))
		}
	}
	for len(lines) < h {
		lines = append(lines, base.Render(""))
	}
	return strings.Join(lines[:h], "\n")
}

// renderForm renders the visible entries with value, layer badge and — for the
// selected entry — description, edit input and validation error.
func (m *Model) renderForm(w, h int) string {
	pal := m.theme()
	rows := m.rows()
	if len(rows) == 0 {
		return lipgloss.NewStyle().Foreground(pal.Secondary).Render("no matching settings")
	}
	clip := lipgloss.NewStyle().MaxWidth(w)
	var lines []string
	for i, r := range rows {
		lines = append(lines, clip.Render(m.renderEntry(r, i == m.sel, w)))
		if i == m.sel {
			detail := "   " + r.entry.Description + "  (" + r.entry.Key + ")"
			if m.invalid != "" {
				detail = "   ✗ " + m.invalid
			}
			style := lipgloss.NewStyle().Foreground(pal.Secondary)
			if m.invalid != "" {
				style = style.Foreground(pal.Error)
			}
			lines = append(lines, clip.Render(style.Render(detail)))
		}
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
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
	case origin == "default":
		style = style.Foreground(pal.Foreground)
	default:
		style = style.Foreground(pal.Info) // overridden values stand out
	}
	return style.Render(line)
}

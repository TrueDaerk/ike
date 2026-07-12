package commitui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/vcs"
)

// View renders the centered dialog box: the changed-files list, the message
// pane, and a footer with the key hints or the blocking reason.
func (m *Model) View() string {
	if !m.open {
		return ""
	}
	pal := m.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	w := m.width * 3 / 4
	if w > 90 {
		w = 90
	}
	if w < 40 {
		w = m.width - 4
	}
	inner := w - 4 // border + padding

	listH := m.height/2 - 8
	if listH < 3 {
		listH = 3
	}
	if listH > len(m.rows) && len(m.rows) > 0 {
		listH = len(m.rows)
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent).Render("Commit Changes")

	var b strings.Builder
	b.WriteString(title + "\n\n")
	b.WriteString(m.renderList(pal, inner, listH))
	b.WriteString("\n" + m.sectionLabel(pal, "Message", m.msgFocus) + "\n")
	b.WriteString(m.renderMessage(pal, inner) + "\n\n")
	b.WriteString(m.footer(pal, inner))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.BorderFocus).
		Background(pal.Panel).
		Padding(0, 1).
		Width(w - 2)
	return box.Render(b.String())
}

// sectionLabel renders a pane heading, highlighted when it holds focus.
func (m *Model) sectionLabel(pal *theme.Palette, text string, focused bool) string {
	s := lipgloss.NewStyle().Foreground(pal.Secondary)
	if focused {
		s = s.Foreground(pal.Accent).Bold(true)
	}
	return s.Render(text)
}

// renderList draws the changed files with their stage checkbox and status
// badge, scrolled around the cursor.
func (m *Model) renderList(pal *theme.Palette, width, height int) string {
	label := m.sectionLabel(pal, "Changes", !m.msgFocus)
	if len(m.rows) == 0 {
		empty := lipgloss.NewStyle().Faint(true).Render("(no changes)")
		return label + "\n" + empty + "\n"
	}
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+height {
		m.top = m.cursor - height + 1
	}
	var b strings.Builder
	b.WriteString(label + "\n")
	for i := m.top; i < m.top+height && i < len(m.rows); i++ {
		r := m.rows[i]
		check := "[ ]"
		if r.Partial {
			check = "[~]"
		} else if r.Staged {
			check = "[x]"
		}
		badge := r.Status.String()
		if badge == "" {
			badge = " "
		}
		line := check + " " + badge + " " + r.Path
		if len(line) > width {
			line = line[:width-1] + "…"
		}
		style := lipgloss.NewStyle().Foreground(pal.Foreground)
		if c := vcs.StatusColor(pal, r.Status); c != nil {
			style = style.Foreground(c)
		}
		if i == m.cursor && !m.msgFocus {
			style = style.Background(pal.Selection).Bold(true)
		}
		b.WriteString(style.Render(line) + "\n")
	}
	return b.String()
}

// renderMessage draws the message pane with a visible cursor block while the
// pane holds focus.
func (m *Model) renderMessage(pal *theme.Palette, width int) string {
	text := m.draft.Text
	cursorAt := m.draft.Pos
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	base := lipgloss.NewStyle().Foreground(pal.Foreground)
	cur := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)
	var b strings.Builder
	seen := 0
	for li, line := range lines {
		rendered := base.Render(line)
		if m.msgFocus && cursorAt >= seen && cursorAt <= seen+len([]rune(line)) {
			at := cursorAt - seen
			r := []rune(line)
			head, tail := string(r[:at]), ""
			cell := " "
			if at < len(r) {
				cell = string(r[at])
				tail = string(r[at+1:])
			}
			rendered = base.Render(head) + cur.Render(cell) + base.Render(tail)
		}
		if line == "" && !(m.msgFocus && cursorAt >= seen && cursorAt <= seen) {
			rendered = lipgloss.NewStyle().Faint(true).Render("")
		}
		b.WriteString(rendered)
		if li < len(lines)-1 {
			b.WriteString("\n")
		}
		seen += len([]rune(line)) + 1
	}
	out := b.String()
	if strings.TrimSpace(text) == "" && !m.msgFocus {
		out = lipgloss.NewStyle().Faint(true).Render("(commit message — tab to edit)")
	}
	return out
}

// footer shows the hints, or the blocking reason when commit is disabled.
func (m *Model) footer(pal *theme.Palette, width int) string {
	hints := "space stage · tab focus · ctrl+s commit · esc close"
	if ok, why := m.canCommit(); !ok {
		hints = why + "  ·  " + hints
		return lipgloss.NewStyle().Foreground(pal.Warning).Render(hints)
	}
	return lipgloss.NewStyle().Faint(true).Render(hints)
}

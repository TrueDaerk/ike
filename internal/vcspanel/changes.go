package vcspanel

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/vcs"
)

// Changes list (Roadmap 0330, #483; slimmed in #750): a read-only list of the
// changed files. The root model answers the emitted messages and re-feeds the
// rows from every status snapshot. Staging, committing and log browsing are
// delegated to custom tool panes (#741, lazygit as the shipped example).

// OpenDiffMsg asks the root model to open the file's diff against HEAD.
type OpenDiffMsg struct{ Path string } // repo-relative

// Row is one changed file in the list.
type Row struct {
	Path   string
	Status vcs.FileStatus
}

// rebuildChanges re-derives the rows from the snapshot, keeping the cursor
// on the same path where possible.
func (m *Model) rebuildChanges() {
	keep := ""
	if m.chCursor < len(m.chRows) {
		keep = m.chRows[m.chCursor].Path
	}
	m.chRows = nil
	if m.snap != nil {
		for _, e := range m.snap.Entries {
			m.chRows = append(m.chRows, Row{Path: e.Path, Status: e.Status})
		}
		sort.Slice(m.chRows, func(i, j int) bool { return m.chRows[i].Path < m.chRows[j].Path })
	}
	m.chCursor = 0
	for i, r := range m.chRows {
		if r.Path == keep {
			m.chCursor = i
			break
		}
	}
}

// updateChanges handles key presses on the list.
func (m *Model) updateChanges(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if m.chCursor < len(m.chRows)-1 {
			m.chCursor++
		}
	case "k", "up":
		if m.chCursor > 0 {
			m.chCursor--
		}
	case "enter":
		if m.chCursor < len(m.chRows) {
			path := m.chRows[m.chCursor].Path
			return func() tea.Msg { return OpenDiffMsg{Path: path} }
		}
	}
	return nil
}

// viewChanges renders the file list plus the footer hints.
func (m *Model) viewChanges() string {
	pal := m.theme()
	listH := m.bodyHeight() - 1 // footer takes one row
	if listH < 1 {
		listH = 1
	}
	var b strings.Builder
	b.WriteString(m.renderChangeRows(pal, listH))
	b.WriteString(m.changesFooter(pal))
	return b.String()
}

// renderChangeRows draws the file list scrolled around the cursor.
func (m *Model) renderChangeRows(pal *theme.Palette, height int) string {
	if len(m.chRows) == 0 {
		return lipgloss.NewStyle().Faint(true).Render(" (working tree clean)") + strings.Repeat("\n", height)
	}
	if m.chCursor < m.chTop {
		m.chTop = m.chCursor
	}
	if m.chCursor >= m.chTop+height {
		m.chTop = m.chCursor - height + 1
	}
	base := lipgloss.NewStyle().Foreground(pal.Foreground) // built once (#1100)
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.chTop + k
		if i < len(m.chRows) {
			r := m.chRows[i]
			badge := r.Status.String()
			if badge == "" {
				badge = " "
			}
			line := " " + badge + " " + r.Path
			style := base
			if c := vcs.StatusColor(pal, r.Status); c != nil {
				style = style.Foreground(c)
			}
			if i == m.chCursor {
				if m.focused {
					style = style.Background(pal.Selection).Bold(true)
				} else {
					// Muted cursor row while unfocused (#1034).
					style = style.Background(pal.SelectionMuted)
				}
			}
			b.WriteString(style.Render(m.clip(line)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// changesFooter shows the key hints.
func (m *Model) changesFooter(pal *theme.Palette) string {
	return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter diff · j/k move"))
}

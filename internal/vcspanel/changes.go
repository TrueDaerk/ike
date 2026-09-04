package vcspanel

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/ui"
	"ike/internal/vcs"
)

// Changes list (Roadmap 0330, #483; slimmed in #750): a read-only list of the
// changed files. The root model answers the emitted messages and re-feeds the
// rows from every status snapshot. Staging, committing and log browsing are
// delegated to custom tool panes (#741, lazygit as the shipped example).

// OpenDiffMsg asks the root model to open the file's diff against HEAD.
type OpenDiffMsg struct{ Path string } // repo-relative

// Row is one changed file in the list. Code is the porcelain badge from the
// entry ("A", "M", "AM"…, #1868); it tells a fully staged file from one that
// was edited again after staging.
type Row struct {
	Path   string
	Status vcs.FileStatus
	Code   string
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
			m.chRows = append(m.chRows, Row{Path: e.Path, Status: e.Status, Code: e.Code()})
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
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if ui.ListNav(msg.String(), &m.chCursor, len(m.chRows), m.changesListHeight(), ui.NavFull) {
		return nil
	}
	switch msg.String() {
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
	var b strings.Builder
	b.WriteString(m.renderChangeRows(pal, m.changesListHeight()))
	b.WriteString(m.changesFooter(pal))
	return b.String()
}

// changesListHeight is the file list's visible row count — the footer takes
// one row off the body. It is the pgup/pgdn page size (#1666).
func (m *Model) changesListHeight() int { return max(1, m.bodyHeight()-1) }

// renderChangeRows draws the file list scrolled around the cursor.
func (m *Model) renderChangeRows(pal *theme.Palette, height int) string {
	if len(m.chRows) == 0 {
		return lipgloss.NewStyle().Faint(true).Render(" (working tree clean)") + strings.Repeat("\n", height)
	}
	m.chTop = ui.ScrollToShow(m.chTop, m.chCursor, height, len(m.chRows))
	base := lipgloss.NewStyle().Foreground(pal.Foreground) // built once (#1100)
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.chTop + k
		if i < len(m.chRows) {
			r := m.chRows[i]
			badge := r.Code
			if badge == "" {
				badge = r.Status.String()
			}
			// Two badge cells so "AM" rows keep the column aligned (#1868).
			line := " " + fmt.Sprintf("%-2s", badge) + " " + r.Path
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

package vcspanel

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/vcs"
)

// Changes view (Roadmap 0330, #483): the persistent staging list plus the
// commit message, sharing the message draft with the modal commit dialog.
// The root model answers the emitted messages with the 0320 git ops and
// re-feeds the rows from every status snapshot.

// ToggleMsg asks the root model to stage (Stage=true) or unstage one file.
type ToggleMsg struct {
	Path  string
	Stage bool
}

// SubmitMsg asks the root model to run the commit with the typed message.
type SubmitMsg struct{ Message string }

// HintMsg surfaces a validation hint as a toast.
type HintMsg struct{ Text string }

// OpenDiffMsg asks the root model to open the file's diff against HEAD.
type OpenDiffMsg struct{ Path string } // repo-relative

// Row is one changed file in the list.
type Row struct {
	Path    string
	Status  vcs.FileStatus
	Staged  bool
	Partial bool
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
			m.chRows = append(m.chRows, Row{
				Path:    e.Path,
				Status:  e.Status,
				Staged:  e.Staged(),
				Partial: e.PartiallyStaged(),
			})
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
	if m.msgFocus && m.snap == nil {
		m.msgFocus = false
	}
}

// stagedCount counts the rows that would land in the commit.
func (m *Model) stagedCount() int {
	n := 0
	for _, r := range m.chRows {
		if r.Staged {
			n++
		}
	}
	return n
}

// canCommit reports whether commit is enabled, with the blocking hint.
func (m *Model) canCommit() (bool, string) {
	if m.stagedCount() == 0 {
		return false, "nothing staged — space toggles a file"
	}
	if strings.TrimSpace(m.draft.Text) == "" {
		return false, "commit message is empty"
	}
	return true, ""
}

// updateChanges handles keys while the Changes tab is visible.
func (m *Model) updateChanges(msg tea.KeyPressMsg) tea.Cmd {
	if m.msgFocus {
		return m.updateChangesMessage(msg)
	}
	switch msg.String() {
	case "j", "down":
		if m.chCursor < len(m.chRows)-1 {
			m.chCursor++
		}
	case "k", "up":
		if m.chCursor > 0 {
			m.chCursor--
		}
	case "space":
		if m.chCursor < len(m.chRows) {
			r := m.chRows[m.chCursor]
			// Optimistic flip; the follow-up refresh restores git's truth.
			stage := !r.Staged || r.Partial
			m.chRows[m.chCursor].Staged = stage
			m.chRows[m.chCursor].Partial = false
			return func() tea.Msg { return ToggleMsg{Path: r.Path, Stage: stage} }
		}
	case "enter":
		if m.chCursor < len(m.chRows) {
			path := m.chRows[m.chCursor].Path
			return func() tea.Msg { return OpenDiffMsg{Path: path} }
		}
	case "c", "m":
		m.msgFocus = true
	case "ctrl+s":
		return m.submit()
	}
	return nil
}

// updateChangesMessage routes keys into the shared draft while the message
// field holds focus; esc returns to the list, ctrl+s commits.
func (m *Model) updateChangesMessage(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.msgFocus = false
		return nil
	case "ctrl+s":
		return m.submit()
	}
	m.draft.Edit(msg)
	return nil
}

// submit emits the commit request, or the blocking hint.
func (m *Model) submit() tea.Cmd {
	if ok, hint := m.canCommit(); !ok {
		return func() tea.Msg { return HintMsg{Text: "cannot commit: " + hint} }
	}
	message := m.draft.Text
	return func() tea.Msg { return SubmitMsg{Message: message} }
}

// viewChanges renders the staging list, the message field, and the footer.
func (m *Model) viewChanges() string {
	pal := m.theme()
	msgLines := strings.Count(m.draft.Text, "\n") + 1
	if msgLines > 4 {
		msgLines = 4
	}
	listH := m.bodyHeight() - msgLines - 2 // message label+footer share rows
	if listH < 1 {
		listH = 1
	}

	var b strings.Builder
	b.WriteString(m.renderChangeRows(pal, listH))
	b.WriteString(m.renderChangesMessage(pal))
	b.WriteString("\n" + m.changesFooter(pal))
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
			line := " " + check + " " + badge + " " + r.Path
			style := base
			if c := vcs.StatusColor(pal, r.Status); c != nil {
				style = style.Foreground(c)
			}
			if i == m.chCursor && !m.msgFocus {
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

// renderChangesMessage draws the one-to-four-line message field.
func (m *Model) renderChangesMessage(pal *theme.Palette) string {
	label := lipgloss.NewStyle().Foreground(pal.Secondary)
	if m.msgFocus {
		label = label.Foreground(pal.Accent).Bold(true)
	}
	text := m.draft.Text
	body := lipgloss.NewStyle().Foreground(pal.Foreground).Render(text)
	if strings.TrimSpace(text) == "" {
		hint := "(commit message — press c to edit)"
		if m.msgFocus {
			hint = "(type the commit message; esc returns to the list)"
		}
		body = lipgloss.NewStyle().Faint(true).Render(hint)
	}
	lines := strings.Split(body, "\n")
	if len(lines) > 4 {
		lines = append(lines[:3], "…")
	}
	return label.Render(" Message: ") + strings.Join(lines, "\n ")
}

// changesFooter shows key hints, or the blocking reason.
func (m *Model) changesFooter(pal *theme.Palette) string {
	hints := " space stage · enter diff · c message · ctrl+s commit"
	if ok, why := m.canCommit(); !ok {
		return lipgloss.NewStyle().Foreground(pal.Warning).Render(m.clip(" " + why + " · " + strings.TrimSpace(hints)))
	}
	return lipgloss.NewStyle().Faint(true).Render(m.clip(hints))
}

// clip bounds one rendered line to the panel width.
func (m *Model) clip(s string) string {
	if m.width > 0 && len([]rune(s)) > m.width {
		return string([]rune(s)[:m.width-1]) + "…"
	}
	return s
}

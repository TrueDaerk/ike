package vcspanel

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/vcs"
)

// Log view (Roadmap 0330, #484): the windowed commit history. Commits load
// page-wise through the root model (the panel never runs git); enter expands
// a commit's changed files, enter on a file opens its parent-vs-commit diff.

// logPageSize is one log window.
const logPageSize = 50

// LogRequestMsg asks the root model to load one log window into the panel.
type LogRequestMsg struct {
	Offset int
	Limit  int
}

// ShowRequestMsg asks the root model to load one commit's details.
type ShowRequestMsg struct{ Hash string }

// OpenCommitDiffMsg asks the root model to open one commit file's diff
// against the commit's parent.
type OpenCommitDiffMsg struct {
	Hash    string
	Path    string
	OldPath string
}

// logRow is one flattened list row: a commit, or a file of the expanded one.
type logRow struct {
	commit int // index into logEntries
	file   int // -1 for the commit row itself
}

// ApplyLog ingests one loaded window (append for follow-up pages, replace
// for offset 0 — the reload path after a mutating command).
func (m *Model) ApplyLog(msg vcs.LogMsg) {
	m.logLoading = false
	if msg.Err != nil {
		m.logErr = msg.Err.Error()
		return
	}
	m.logErr = ""
	if msg.Offset == 0 {
		m.logEntries = msg.Entries
		m.logCursor, m.logTop = 0, 0
		m.expandedHash = ""
		m.details = nil
	} else {
		m.logEntries = append(m.logEntries, msg.Entries...)
	}
	m.logHasMore = msg.HasMore
	m.rebuildLogRows()
}

// ApplyShow ingests one commit's details and expands it.
func (m *Model) ApplyShow(msg vcs.ShowMsg) {
	if msg.Err != nil {
		m.logErr = msg.Err.Error()
		return
	}
	m.logErr = ""
	m.expandedHash = msg.Entry.Hash
	m.details = &msg
	m.rebuildLogRows()
}

// rebuildLogRows flattens commits plus the expanded commit's files.
func (m *Model) rebuildLogRows() {
	m.logRows = m.logRows[:0]
	for ci := range m.logEntries {
		m.logRows = append(m.logRows, logRow{commit: ci, file: -1})
		if m.details != nil && m.logEntries[ci].Hash == m.expandedHash {
			for fi := range m.details.Files {
				m.logRows = append(m.logRows, logRow{commit: ci, file: fi})
			}
		}
	}
	if m.logCursor >= len(m.logRows) {
		m.logCursor = len(m.logRows) - 1
	}
	if m.logCursor < 0 {
		m.logCursor = 0
	}
}

// ensureLogLoaded requests the first window when the tab opens empty.
func (m *Model) ensureLogLoaded() tea.Cmd {
	if m.snap == nil || m.logLoading || len(m.logEntries) > 0 {
		return nil
	}
	m.logLoading = true
	return func() tea.Msg { return LogRequestMsg{Offset: 0, Limit: logPageSize} }
}

// ReloadLog re-requests the first window (after commit/update/checkout); a
// never-opened log stays lazy.
func (m *Model) ReloadLog() tea.Cmd {
	if m.snap == nil || len(m.logEntries) == 0 {
		return nil
	}
	m.logLoading = true
	return func() tea.Msg { return LogRequestMsg{Offset: 0, Limit: logPageSize} }
}

// updateLog handles keys while the Log tab is visible.
func (m *Model) updateLog(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if m.logCursor < len(m.logRows)-1 {
			m.logCursor++
		} else if m.logHasMore && !m.logLoading {
			// The cursor hit the loaded tail: fetch the next window.
			m.logLoading = true
			offset := len(m.logEntries)
			return func() tea.Msg { return LogRequestMsg{Offset: offset, Limit: logPageSize} }
		}
	case "k", "up":
		if m.logCursor > 0 {
			m.logCursor--
		}
	case "r":
		return m.ReloadLog()
	case "enter":
		return m.activateLogRow()
	}
	return nil
}

// activateLogRow expands/collapses a commit, or opens a file's diff.
func (m *Model) activateLogRow() tea.Cmd {
	if m.logCursor >= len(m.logRows) {
		return nil
	}
	row := m.logRows[m.logCursor]
	entry := m.logEntries[row.commit]
	if row.file >= 0 {
		f := m.details.Files[row.file]
		return func() tea.Msg {
			return OpenCommitDiffMsg{Hash: entry.Hash, Path: f.Path, OldPath: f.OldPath}
		}
	}
	if m.expandedHash == entry.Hash {
		// Collapse.
		m.expandedHash = ""
		m.details = nil
		m.rebuildLogRows()
		return nil
	}
	hash := entry.Hash
	return func() tea.Msg { return ShowRequestMsg{Hash: hash} }
}

// viewLog renders the flattened rows scrolled around the cursor.
func (m *Model) viewLog() string {
	pal := m.theme()
	if m.logErr != "" {
		return lipgloss.NewStyle().Foreground(pal.Error).Render(" log: " + m.logErr)
	}
	if len(m.logRows) == 0 {
		text := " (no commits)"
		if m.logLoading {
			text = " loading…"
		}
		return lipgloss.NewStyle().Faint(true).Render(text)
	}
	height := m.bodyHeight() - 2 // header + footer
	if height < 1 {
		height = 1
	}
	if m.logCursor < m.logTop {
		m.logTop = m.logCursor
	}
	if m.logCursor >= m.logTop+height {
		m.logTop = m.logCursor - height + 1
	}
	now := time.Now()
	cols := m.logColumns()
	var b strings.Builder
	b.WriteString(m.logHeader(pal, cols))
	b.WriteString("\n")
	for k := 0; k < height; k++ {
		i := m.logTop + k
		if i < len(m.logRows) {
			b.WriteString(m.renderLogRow(pal, i, now, cols))
		}
		b.WriteString("\n")
	}
	b.WriteString(m.logFooter(pal))
	return b.String()
}

// logCols is the tabular layout of the commit list (#501): fixed hash,
// author, and date columns around a flexible subject.
type logCols struct {
	hash, subject, author, date int
}

// logColumns budgets the columns for the current panel width. The subject
// keeps priority: on narrow panels the date, then the author drop to zero
// and disappear entirely.
func (m *Model) logColumns() logCols {
	c := logCols{hash: 9, author: 14, date: 14} // hash: "▸ " + 7 short
	fixed := 1 + c.hash + 2 + c.author + 2 + c.date
	c.subject = m.width - fixed
	if c.subject < 20 {
		c.date = 0
		c.subject = m.width - (1 + c.hash + 2 + c.author + 2)
	}
	if c.subject < 20 {
		c.author = 0
		c.subject = m.width - (1 + c.hash + 2)
	}
	if c.subject < 1 {
		c.subject = 1
	}
	return c
}

// cell pads or clips s to exactly width display cells (width 0 hides it).
func cell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > width {
		if width == 1 {
			return "…"
		}
		return string(r[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-len(r))
}

// logHeader renders the faint column caption row.
func (m *Model) logHeader(pal *theme.Palette, c logCols) string {
	var b strings.Builder
	b.WriteString(" " + cell("Commit", c.hash))
	b.WriteString("  " + cell("Subject", c.subject))
	if c.author > 0 {
		b.WriteString("  " + cell("Author", c.author))
	}
	if c.date > 0 {
		b.WriteString("  " + cell("Date", c.date))
	}
	return lipgloss.NewStyle().Faint(true).Underline(true).Render(b.String())
}

// renderLogRow draws one flattened row.
func (m *Model) renderLogRow(pal *theme.Palette, i int, now time.Time, cols logCols) string {
	row := m.logRows[i]
	entry := m.logEntries[row.commit]
	selected := i == m.logCursor && m.focused

	var line string
	var style lipgloss.Style
	if row.file < 0 {
		marker := "▸"
		if entry.Hash == m.expandedHash {
			marker = "▾"
		}
		var b strings.Builder
		b.WriteString(" " + cell(marker+" "+entry.ShortHash, cols.hash))
		b.WriteString("  " + cell(entry.Subject, cols.subject))
		if cols.author > 0 {
			b.WriteString("  " + cell(entry.Author, cols.author))
		}
		if cols.date > 0 {
			b.WriteString("  " + cell(vcs.RelativeTime(entry.Time, now), cols.date))
		}
		line = b.String()
		style = lipgloss.NewStyle().Foreground(pal.Foreground)
	} else {
		f := m.details.Files[row.file]
		badge := f.Status.String()
		if badge == "" {
			badge = " "
		}
		line = "      " + badge + " " + f.Path
		style = lipgloss.NewStyle().Foreground(pal.Foreground)
		if c := vcs.StatusColor(pal, f.Status); c != nil {
			style = style.Foreground(c)
		}
	}
	if selected {
		style = style.Background(pal.Selection).Bold(true)
	}
	return style.Render(m.clip(line))
}

// logFooter shows the hints plus the paging/loading state.
func (m *Model) logFooter(pal *theme.Palette) string {
	hints := " enter expand/diff · j/k move · r reload"
	switch {
	case m.logLoading:
		hints += " · loading…"
	case m.logHasMore:
		hints += " · j past the end loads more"
	}
	return lipgloss.NewStyle().Faint(true).Render(m.clip(hints))
}

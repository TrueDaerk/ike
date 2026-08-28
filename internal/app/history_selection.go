package app

// history_selection.go implements Show History for Selection (#1430),
// JetBrains' Git > Show History for Selection: with an active visual
// selection (or the caret line as fallback) the editor's line range goes
// through `git log -L` (vcs.RangeLogCmd), and the commits that touched
// exactly that range show in the modal shell — enter expands one commit to
// the patch git computed for the tracked range.

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/project"
	"ike/internal/ui"
	"ike/internal/vcs"
)

// historyHeading prefixes the shell heading in both picker views, so the
// open-guard survives the list/patch switch (the localHistoryOpen pattern).
const historyHeading = "RANGE HISTORY — "

// historyForSelection validates the focused file and starts the range query:
// the visual selection's lines, else the caret line.
func (m Model) historyForSelection() (tea.Model, tea.Cmd) {
	snap := m.vcs.snap
	if snap == nil {
		m.host.Notify(host.Info, "not a git repository")
		return m, nil
	}
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "no file for history")
		return m, nil
	}
	path := ed.Path()
	if snap.Status(path) == vcs.StatusUntracked {
		m.host.Notify(host.Warn, "untracked file — there is no committed history yet")
		return m, nil
	}
	start, end, ok := ed.SelectionLines()
	if !ok {
		start, _ = ed.Cursor()
		end = start
	}
	rel, err := filepath.Rel(snap.Root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		m.host.Notify(host.Info, "file is outside the repository")
		return m, nil
	}
	return m, vcs.RangeLogCmd(snap.Root, rel, start, end)
}

// openHistoryPicker lands the range query result in the modal shell.
func (m *Model) openHistoryPicker(msg vcs.RangeLogMsg) {
	if msg.Err != nil {
		m.host.Notify(host.Warn, "history for selection: "+msg.Err.Error())
		return
	}
	rangeLabel := "line " + strconv.Itoa(msg.StartLine)
	if msg.EndLine > msg.StartLine {
		rangeLabel = "lines " + strconv.Itoa(msg.StartLine) + "–" + strconv.Itoa(msg.EndLine)
	}
	if len(msg.Entries) == 0 {
		m.host.Notify(host.Info, "no history for "+rangeLabel+" of "+filepath.Base(msg.Path))
		return
	}
	m.histResult = msg
	m.histLabel = rangeLabel
	m.histSel = 0
	m.histPatch = false
	m.histPicker = true
	m.setHistoryListContent()
	m.shell.Open()
}

// setHistoryListContent (re-)binds the shell body to THIS model copy. The
// root model is a value model, so a closure bound once at open time would
// keep rendering the open-time selection; every selection change re-binds.
func (m *Model) setHistoryListContent() {
	m.shell.SetContent(ui.ModelContent{
		Heading: historyHeading + filepath.Base(m.histResult.Path) + " (" + m.histLabel + ")",
		Body:    m.renderHistoryList,
	})
}

// historyPickerOpen reports whether the shell currently shows the range
// history picker (either view).
func (m Model) historyPickerOpen() bool {
	if !m.histPicker || !m.shell.IsOpen() {
		return false
	}
	c, ok := m.shell.Content().(ui.ModelContent)
	return ok && strings.HasPrefix(c.Heading, historyHeading)
}

// renderHistoryList draws the commit list, newest first.
func (m *Model) renderHistoryList() string {
	var b strings.Builder
	now := time.Now()
	for i, e := range m.histResult.Entries {
		marker := "  "
		if i == m.histSel {
			marker = "▍ "
		}
		fmt.Fprintf(&b, "%s%s  %-8s %-16s %s\n", marker, e.ShortHash,
			project.RelTime(e.Time, now), truncatePad(e.Author, 16), e.Subject)
	}
	if m.histResult.Truncated {
		b.WriteString("\n(older history truncated)\n")
	}
	b.WriteString("\nenter show patch · j/k move · esc close")
	return b.String()
}

// renderHistoryPatch draws one commit's metadata plus the patch git computed
// for the tracked range in that commit.
func (m *Model) renderHistoryPatch() string {
	e := m.histResult.Entries[m.histSel]
	var b strings.Builder
	fmt.Fprintf(&b, "commit %s\nauthor %s\ndate   %s\n\n%s\n\n",
		e.ShortHash, e.Author, e.Time.Local().Format("2006-01-02 15:04:05"), e.Subject)
	b.WriteString(e.Patch)
	b.WriteString("\n\nesc back to list · j/k scroll")
	return b.String()
}

// truncatePad clips s to width runes, padding shorter values.
func truncatePad(s string, width int) string {
	r := []rune(s)
	if len(r) > width {
		return string(r[:width-1]) + "…"
	}
	return s
}

// updateHistoryPicker consumes every key while the picker is open: list
// navigation, patch expansion, and scrolling inside the patch view.
func (m Model) updateHistoryPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.histPatch {
		switch msg.String() {
		case "esc", "q", "backspace", "h", "left":
			m.histPatch = false
			m.setHistoryListContent()
			return m, nil
		}
		// Everything else (j/k, arrows, page keys) scrolls the patch.
		m.shell.Update(msg)
		return m, nil
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp.
	if m.pickerNav(msg.String(), &m.histSel, len(m.histResult.Entries), m.setHistoryListContent) {
		return m, nil
	}
	switch msg.String() {
	case "esc", "q":
		m.histPicker = false
		m.shell.Close()
		return m, nil
	case "enter", "l", "right":
		m.histPatch = true
		e := m.histResult.Entries[m.histSel]
		m.shell.SetContent(ui.ModelContent{
			Heading: historyHeading + e.ShortHash + " — " + e.Subject,
			Body:    m.renderHistoryPatch,
		})
		return m, nil
	}
	return m, nil
}

// historyPickerClickRow maps a body row of the range-history list onto a
// commit (#2275): rows 0..n-1 are commits, the truncation note and the key
// hints below them are inert, and the patch view has no rows to click at all.
// A click selects; a click on the already-selected commit shows its patch,
// the picker's enter.
func (m Model) historyPickerClickRow(row int) (tea.Model, tea.Cmd) {
	if m.histPatch || row < 0 || row >= len(m.histResult.Entries) {
		return m, nil
	}
	if row == m.histSel {
		m.histPatch = true
		e := m.histResult.Entries[m.histSel]
		m.shell.SetContent(ui.ModelContent{
			Heading: historyHeading + e.ShortHash + " — " + e.Subject,
			Body:    m.renderHistoryPatch,
		})
		return m, nil
	}
	m.histSel = row
	m.setHistoryListContent()
	return m, nil
}

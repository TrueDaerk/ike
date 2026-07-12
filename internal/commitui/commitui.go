// Package commitui is the commit dialog (Roadmap 0320, #465): a centered
// overlay listing the changed files with stage toggles next to a commit
// message editor, JetBrains' Commit dialog scaled to the terminal. The root
// model routes keys here while open, runs the emitted stage/commit commands,
// and re-feeds the file list after every status refresh. The in-progress
// message survives Esc and reopen; only a successful commit clears it.
package commitui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
	"ike/internal/vcs"
)

// ToggleMsg asks the root model to stage (Stage=true) or unstage one file.
type ToggleMsg struct {
	Path  string
	Stage bool
}

// SubmitMsg asks the root model to run the commit with the typed message.
type SubmitMsg struct{ Message string }

// HintMsg surfaces a validation hint (nothing staged, empty message) as a
// toast without closing the dialog.
type HintMsg struct{ Text string }

// Row is one changed file in the list.
type Row struct {
	Path    string
	Status  vcs.FileStatus
	Staged  bool
	Partial bool // staged with further unstaged edits on top
}

// Model is the dialog state.
type Model struct {
	open   bool
	rows   []Row
	cursor int
	top    int

	// draft is the shared in-progress commit message (0330, #483): the VCS
	// tool window edits the same draft, and it clears only on ClearMessage
	// (successful commit). msgFocus routes typing into the message pane.
	draft    *vcs.MessageDraft
	msgFocus bool

	width, height int
	pal           *theme.Palette
}

// New returns a closed dialog.
func New() *Model { return &Model{draft: &vcs.MessageDraft{}} }

// SetPalette threads the active theme in.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetSize records the terminal size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// Open shows the dialog over the given changed files. The retained message
// is kept; the cursor resets to the top.
func (m *Model) Open(rows []Row) {
	m.open = true
	m.cursor, m.top = 0, 0
	m.msgFocus = len(rows) == 0
	m.SetRows(rows)
}

// SetRows replaces the file list (after a status refresh), keeping the cursor
// on the same path where possible.
func (m *Model) SetRows(rows []Row) {
	keep := ""
	if m.cursor < len(m.rows) {
		keep = m.rows[m.cursor].Path
	}
	m.rows = rows
	if keep != "" {
		for i, r := range rows {
			if r.Path == keep {
				m.cursor = i
				break
			}
		}
	}
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// Close hides the dialog; the in-progress message is retained by design.
func (m *Model) Close() { m.open = false }

// IsOpen reports whether the dialog is shown.
func (m *Model) IsOpen() bool { return m.open }

// SetDraft swaps the backing message store for the shared draft (0330,
// #483), so the dialog and the VCS tool window edit the same text.
func (m *Model) SetDraft(d *vcs.MessageDraft) {
	if d != nil {
		m.draft = d
	}
}

// ClearMessage drops the retained commit message (after a successful commit).
func (m *Model) ClearMessage() { m.draft.Clear() }

// Message exposes the in-progress message (tests).
func (m *Model) Message() string { return m.draft.Text }

// stagedCount counts the rows that would land in the commit.
func (m *Model) stagedCount() int {
	n := 0
	for _, r := range m.rows {
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

// Update handles one key while the dialog is open.
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.Close() // message retained
		return nil
	case "tab":
		m.msgFocus = !m.msgFocus
		return nil
	case "ctrl+s":
		if ok, hint := m.canCommit(); !ok {
			return func() tea.Msg { return HintMsg{Text: "cannot commit: " + hint} }
		}
		message := m.draft.Text
		return func() tea.Msg { return SubmitMsg{Message: message} }
	}
	if m.msgFocus {
		return m.updateMessage(msg)
	}
	return m.updateList(msg)
}

// updateList handles keys while the file list has focus.
func (m *Model) updateList(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "space":
		if m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
			// Optimistic flip so the toggle reads instantly; the follow-up
			// status refresh replaces the rows with git's truth.
			stage := !r.Staged || r.Partial
			m.rows[m.cursor].Staged = stage
			m.rows[m.cursor].Partial = false
			return func() tea.Msg { return ToggleMsg{Path: r.Path, Stage: stage} }
		}
	}
	return nil
}

// updateMessage handles keys while the message pane has focus.
func (m *Model) updateMessage(msg tea.KeyPressMsg) tea.Cmd {
	m.draft.Edit(msg)
	return nil
}

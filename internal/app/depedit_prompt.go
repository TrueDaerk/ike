package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/ui"
)

// depedit_prompt.go drives the confirmation shown before the first edit of a
// dependency file (#565). The editor blocks the edit and emits
// editor.DepEditBlockedMsg; this prompt mirrors the revert-prompt pattern
// (vcs_actions.go) and, on accept, routes editor.ConfirmDepEditMsg to the
// active editor so the blocked edit is unlocked and replayed.

// openDepEditPrompt shows the "this file is not part of your project" dialog.
func (m *Model) openDepEditPrompt(path string) {
	m.depEditPending = path
	m.shell.SetContent(ui.ModelContent{
		Heading: "Edit a file outside your project",
		Body: func() string {
			return displayPath(path) + " lives in a dependency directory, so it is not part of your project.\n\n" +
				"Editing it changes vendored code that a reinstall will overwrite.\n\n" +
				"  [enter] edit anyway — unlock this file for the session\n" +
				"  [esc]   cancel"
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// updateDepEditPrompt consumes every key while the dep-edit prompt is open.
func (m Model) updateDepEditPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y":
		m.depEditPending = ""
		m.shell.Close()
		key := m.activeEditorKey()
		if key == "" {
			return m, nil
		}
		// Replays the blocked edit on the now-unlocked buffer.
		return m, m.activeWS().Panes.Get(key).Update(editor.ConfirmDepEditMsg{})
	case "esc":
		m.depEditPending = ""
		m.shell.Close()
		if ed := m.activeEditor(); ed != nil {
			ed.CancelDepEdit()
		}
		return m, nil
	}
	return m, nil
}

// depEditPromptOpen reports whether the shell shows the dep-edit confirmation.
func (m Model) depEditPromptOpen() bool { return m.depEditPending != "" && m.shell.IsOpen() }

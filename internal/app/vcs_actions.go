package app

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/ui"
	"ike/internal/vcs"
)

// vcs_actions.go drives vcs.updateProject and vcs.revertFile (Roadmap 0320,
// #466): update pulls the upstream per the configured strategy with a summary
// toast; revert restores the active file to HEAD behind a confirmation prompt
// showing what would be lost. UX per the 0082 sheets on #22.

// UpdateProjectMsg runs vcs.updateProject.
type UpdateProjectMsg struct{}

// RevertActiveFileMsg starts the vcs.revertFile flow for the focused editor.
type RevertActiveFileMsg struct{}

// updateProject validates and launches the pull.
func (m Model) updateProject() (tea.Model, tea.Cmd) {
	snap := m.vcs.snap
	if snap == nil {
		m.host.Notify(host.Info, "not a git repository")
		return m, nil
	}
	if len(snap.Entries) > 0 {
		// No surprise loss (0082/29): a dirty tree blocks the update with a
		// clear warning instead of half-applying a merge over local edits.
		m.host.Notify(host.Warn, "working tree has uncommitted changes — commit or stash before updating")
		return m, nil
	}
	strategy := "merge"
	if v, ok := m.host.Config().Get("vcs.update"); ok && v == "rebase" {
		strategy = "rebase"
	}
	m.host.Notify(host.Info, "updating project ("+strategy+")…")
	return m, vcs.UpdateCmd(snap.Root, strategy)
}

// revertActiveFile validates the focused file and asks for the change count
// backing the confirmation prompt.
func (m Model) revertActiveFile() (tea.Model, tea.Cmd) {
	snap := m.vcs.snap
	if snap == nil {
		m.host.Notify(host.Info, "not a git repository")
		return m, nil
	}
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "no file to revert")
		return m, nil
	}
	path := ed.Path()
	switch snap.Status(path) {
	case vcs.StatusUntracked:
		m.host.Notify(host.Warn, "untracked file — there is no committed version to revert to")
		return m, nil
	case vcs.StatusNone:
		hint := "no changes to revert"
		if ed.Dirty() {
			hint += " (unsaved buffer edits stay — undo or save them instead)"
		}
		m.host.Notify(host.Info, hint)
		return m, nil
	}
	return m, vcs.RevertInfoCmd(snap.Root, path)
}

// openRevertPrompt shows the destructive-action confirmation with the line
// count the revert would discard.
func (m *Model) openRevertPrompt(path string, changed int) {
	m.revertPending = path
	m.shell.SetContent(ui.ModelContent{
		Heading: "Revert file to HEAD",
		Body: func() string {
			lines := strconv.Itoa(changed) + " changed lines"
			if changed == 1 {
				lines = "1 changed line"
			}
			return displayPath(path) + ": discard " + lines + " and restore the last committed version?\n\n" +
				"This cannot be undone.\n\n" +
				"  [enter] revert — discard the local changes\n" +
				"  [esc]   cancel"
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// updateRevertPrompt consumes every key while the revert prompt is open.
func (m Model) updateRevertPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y":
		path := m.revertPending
		m.revertPending = ""
		m.shell.Close()
		snap := m.vcs.snap
		if snap == nil {
			return m, nil
		}
		return m, vcs.RevertCmd(snap.Root, path)
	case "esc":
		m.revertPending = ""
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// revertPromptOpen reports whether the shell shows the revert confirmation.
func (m Model) revertPromptOpen() bool { return m.revertPending != "" && m.shell.IsOpen() }

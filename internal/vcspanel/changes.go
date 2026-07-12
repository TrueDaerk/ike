package vcspanel

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Changes view (#483 fills this in): staging list + commit message. The
// skeleton renders a placeholder so the pane lands (#482) before the view.

// updateChanges handles keys while the Changes tab is visible.
func (m *Model) updateChanges(msg tea.KeyPressMsg) tea.Cmd {
	return nil
}

// viewChanges renders the Changes tab body.
func (m *Model) viewChanges() string {
	return lipgloss.NewStyle().Faint(true).Render("changes view lands with #483")
}

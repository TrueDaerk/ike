package vcspanel

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Log view (#484 fills this in): windowed commit history with details. The
// skeleton renders a placeholder so the pane lands (#482) before the view.

// updateLog handles keys while the Log tab is visible.
func (m *Model) updateLog(msg tea.KeyPressMsg) tea.Cmd {
	return nil
}

// viewLog renders the Log tab body.
func (m *Model) viewLog() string {
	return lipgloss.NewStyle().Faint(true).Render("log view lands with #484")
}

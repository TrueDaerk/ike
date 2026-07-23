package app

// termguard.go is the busy-terminal guard on the terminal close path (#986):
// the reserved cmd+w inside a focused terminal sends the shell an EOF when it
// is idle — the shell exits and the regular exit path closes the pane/tab —
// while a running foreground process raises a floating-shell prompt first:
// enter closes (killing the process), esc keeps the terminal.

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// requestTerminalClose handles the reserved cmd+w for the focused terminal:
// idle → EOF to the shell, busy → confirmation prompt.
func (m *Model) requestTerminalClose() {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return
	}
	term := inst.ActiveTerminal()
	if term == nil {
		return
	}
	if term.Busy() {
		m.openTermClosePrompt()
		return
	}
	term.SendEOF()
}

// openTermClosePrompt shows the busy-terminal close guard.
func (m *Model) openTermClosePrompt() {
	m.termClosePending = true
	body := "a process is still running in this terminal.\n\n" +
		"  [enter] close — stop the process\n" +
		"  [esc]   cancel — keep the terminal"
	m.shell.SetContent(ui.ModelContent{
		Heading: "Close terminal?",
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// termClosePromptOpen reports whether the guard currently owns the keyboard.
func (m Model) termClosePromptOpen() bool { return m.termClosePending && m.shell.IsOpen() }

// updateTermClosePrompt consumes every key while the guard is open: enter
// closes the terminal (its process dies with the session), esc cancels.
func (m Model) updateTermClosePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.termClosePending = false
		m.shell.Close()
		m.closeFocused()
		return m, nil
	case "esc":
		m.termClosePending = false
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

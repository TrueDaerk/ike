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
// exited → close outright, idle → EOF to the shell, busy → confirmation
// prompt.
func (m *Model) requestTerminalClose() {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return
	}
	term := inst.ActiveTerminal()
	if term == nil {
		return
	}
	if term.Exited() {
		// The child is gone (#2192): an EOF would reach nobody and the exit
		// path already ran, so the pane/tab must be dropped here instead —
		// closeFocused picks the tab or the whole leaf like ctrl+w does.
		m.closeFocused()
		return
	}
	if term.Busy() {
		m.termCloseSess = term.SessionKey()
		m.openTermClosePrompt()
		return
	}
	term.SendEOF()
}

// openTermClosePrompt shows the busy-terminal close guard. Callers set
// termCloseSess (and termClosePopup for the popup arm) first, so the confirm
// targets the session the guard was raised for (#1786).
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
		sess := m.termCloseSess
		m.termCloseSess = ""
		if m.termClosePopup {
			// The guard was raised for a popup terminal tab (#1398), not the
			// focused pane. Re-resolve by session key (#1786): the busy shell
			// may have exited while the prompt was open — its exit already
			// closed the tab and shifted the active index, and confirming
			// then must not kill an unrelated shell.
			m.termClosePopup = false
			if inst, idx, t := m.popupTabForSession(sess); t != nil {
				m.closePopupTab(inst, idx)
			}
			return m, nil
		}
		// Same staleness guard for the pane arm: the exit path may already
		// have closed the busy pane and moved focus elsewhere (#1786).
		if sess == "" || m.focusedTerminalSession() == sess {
			m.closeFocused()
		}
		return m, nil
	case "esc":
		m.termClosePending = false
		m.termClosePopup = false
		m.termCloseSess = ""
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// focusedTerminalSession is the session key of the focused pane's active
// terminal, "" when the focused pane holds none.
func (m Model) focusedTerminalSession() string {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return ""
	}
	term := inst.ActiveTerminal()
	if term == nil {
		return ""
	}
	return term.SessionKey()
}

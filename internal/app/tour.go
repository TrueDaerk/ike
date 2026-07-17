package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/tour"
)

// tour.go hosts the Welcome Tour (#657): a passive, paged first-orientation
// walkthrough in the floating shell, opened via the help.welcomeTour command
// (palette: "Welcome Tour"). Rendering lives in internal/tour; the paging
// keys are handled here, host-level — the shell scroller owns space/arrows,
// so the tour must never be plain scrollable content (same pattern as the
// LSP onboarding dialog). First-run auto-open is #658.

// ShowWelcomeTourMsg asks the root model to open the welcome tour.
type ShowWelcomeTourMsg struct{}

// openTour shows the tour from its first page.
func (m *Model) openTour() {
	m.tour = tour.New(m.bindings)
	m.shell.SetContent(m.tour)
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// tourOpen reports whether the tour is showing.
func (m Model) tourOpen() bool { return m.tour != nil && m.shell.IsOpen() }

// updateTour consumes every key while the tour is open: right/l/space page
// forward (finishing on the last page), left/h page back, esc closes.
// Everything else is swallowed so nothing leaks past the modal.
func (m Model) updateTour(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "right", "l", "space", " ", "enter":
		if !m.tour.Next() {
			return m.closeTour(), nil // finishing the last page closes
		}
	case "left", "h":
		m.tour.Prev()
	case "esc", "q":
		return m.closeTour(), nil
	}
	return m, nil
}

// closeTour dismisses the tour.
func (m Model) closeTour() tea.Model {
	m.tour = nil
	m.shell.Close()
	return m
}

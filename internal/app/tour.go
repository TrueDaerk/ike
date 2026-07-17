package app

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
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

// tourAutoOpen gates the first-run scan. Unlike the other startup prompts
// (crash recovery needs snapshots, LSP onboarding needs auto_install +
// installable servers) the tour is due on EVERY first start — which is also
// what every test building a model on a fresh config dir looks like, where an
// auto-opened modal would swallow all scripted keys and mouse events. The
// package's TestMain turns the scan off; the first-run tests turn it back on.
// Production never touches it.
var tourAutoOpen = true

// scanTour decides at startup whether the first-run tour is due: a first
// start (no user settings file yet) that has not been marked onboarded. The
// tour itself waits for the first window size (maybeOpenTour).
func (m *Model) scanTour() {
	if !tourAutoOpen || m.cfgOpts.UserPath == "" {
		return
	}
	if _, err := os.Stat(m.cfgOpts.UserPath); err == nil {
		return // an existing config is not a first start
	}
	if c := config.Get(); c == nil || c.UI.Onboarded {
		return
	}
	m.tourPending = true
}

// maybeOpenTour shows the first-run tour once the window is sized, if startup
// flagged it and no crash-recovery prompt holds the shell. It runs before
// maybeOpenOnboarding, so the LSP dialog naturally queues behind the tour.
// The returned command persists ui.onboarded immediately — on OPEN, not on
// close — so quitting mid-tour cannot leave a half-created settings file that
// suppresses the LSP dialog on the next launch.
func (m *Model) maybeOpenTour() tea.Cmd {
	if !m.tourPending || m.tour != nil || m.width == 0 || m.height == 0 {
		return nil
	}
	if m.recovery != nil || len(m.recoveryPending) > 0 || m.shell.IsOpen() {
		return nil
	}
	m.tourPending = false
	m.openTour()
	return config.WriteAndReload(m.cfgOpts, config.UserScope, "ui.onboarded", true)
}

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

// closeTour dismisses the tour and lets the next queued startup prompt (the
// first-start LSP onboarding dialog, #658) take the freed shell — its
// maybeOpen refuses while the shell is open, so the handoff must be explicit.
func (m Model) closeTour() tea.Model {
	m.tour = nil
	m.shell.Close()
	m.maybeOpenOnboarding()
	return m
}

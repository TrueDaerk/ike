package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/tour"
	"ike/internal/ui"
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

// scanTour decides at startup whether the first-run tour is due: the
// ui.onboarded flag alone gates it (#671) — NOT the settings file's
// existence, because main records the project open into the recent-projects
// history before the model is built, so the user settings file exists on
// every launch, including the very first. The tour itself waits for the
// first window size (maybeOpenTour).
func (m *Model) scanTour() {
	if !tourAutoOpen || m.cfgOpts.UserPath == "" {
		return
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
	m.showTourShell()
}

// showTourShell (re)installs the tour as the floating shell's content — on
// open, and on resume after a try-it overlay (#680) took the shell or the
// screen.
func (m *Model) showTourShell() {
	m.shell.SetContent(m.tour)
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// tourOpen reports whether the tour is showing: it exists AND the shell is
// open on it. A try-it key (#680) may hand the shell to other content (f1
// help) — the tour is then suspended, not open, and keys must not route to
// it.
func (m Model) tourOpen() bool {
	return m.tour != nil && m.shell.IsOpen() && m.shell.Content() == ui.Content(m.tour)
}

// tourSuspended reports whether a tour exists but is not showing — a try-it
// overlay (palette, search everywhere, f1 help) covers it or took the shell.
// maybeResumeTour brings it back once the screen is free.
func (m Model) tourSuspended() bool { return m.tour != nil && !m.tourOpen() }

// maybeResumeTour reopens a suspended tour once no overlay holds the screen
// (#680): the palette family is closed and the shell is free. It returns to
// the page the user left, with the completed task ticked.
func (m *Model) maybeResumeTour() {
	if m.tourSuspended() && !m.shell.IsOpen() && !m.palette.IsOpen() && !m.finder.IsOpen() {
		m.showTourShell()
	}
}

// updateTour handles a key while the tour is showing. Paging keys always
// belong to the tour: right/l/space page forward (finishing on the last page),
// left/h page back, esc/q closes — pages stay skippable regardless of task
// state. Any other key is swallowed on passive pages; on a page with an
// unfinished try-it task it is NOT consumed (#680) — the caller lets it fall
// through to normal key handling so the taught chord really drives the app,
// and the command-executed signal (#679) ticks the task.
func (m Model) updateTour(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "right", "l", "space", " ", "enter":
		if !m.tour.Next() {
			// Finishing the last page closes the tour and starts the setup
			// flow (#713): theme picker → LSP servers → toolchain check.
			return m.finishTour(), nil, true
		}
	case "left", "h":
		m.tour.Prev()
	case "esc", "q":
		return m.closeTour(), nil, true
	default:
		if m.tour.HasPendingTasks() {
			return m, nil, false // try-it pass-through (#680)
		}
	}
	return m, nil, true
}

// closeTour dismisses the tour (esc/q — a skip) and lets the next queued
// startup prompt (the first-start LSP onboarding dialog, #658) take the freed
// shell — its maybeOpen refuses while the shell is open, so the handoff must
// be explicit.
func (m Model) closeTour() tea.Model {
	m.tour = nil
	m.shell.Close()
	m.maybeOpenOnboarding()
	return m
}

// finishTour dismisses the tour after its last page and starts the setup
// flow (#713). The flow's forced LSP step replaces the first-run pending
// dialog (startSetupFlow clears the flag).
func (m Model) finishTour() tea.Model {
	m.tour = nil
	m.shell.Close()
	m.startSetupFlow()
	return m
}

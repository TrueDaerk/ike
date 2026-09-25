package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
	"ike/internal/testresults"
)

// tests_panel.go wires the Test Results tool window (#1911): a singleton
// bottom-split pane showing the last captured test run as a structured tree,
// mirroring the Problems panel's toggle state machine. The pane is a pure
// consumer — testrun.go captures and parses the run and fills it in.

// TestsToggleMsg runs tests.toggle.
type TestsToggleMsg struct{}

// toggleTestsPanel is the tests.toggle state machine, mirroring
// toggleProblemsPanel: no panel → open at the bottom; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleTestsPanel() {
	m.togglePanel(pane.TestsKey, func() tea.Cmd { m.openTestsPanel(); return nil })
}

// testsPanel returns the singleton panel model, or nil when it is not open.
func (m Model) testsPanel() *testresults.Model {
	if inst := m.toolWindow(pane.KindTests); inst != nil {
		return inst.Tests()
	}
	return nil
}

// openTestsPanel splits the active editor (fallback: focused leaf) at the
// adaptive placement (auxZone, #1588) with the singleton panel.
func (m *Model) openTestsPanel() {
	m.openToolPane(m.activeWS().Panes.AddTests, m.auxZone, nil)
}

package app

import (
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
	if !m.activeWS().Panes.Has(pane.TestsKey) {
		m.testsReturnFocus = m.activeWS().Panes.Focused()
		m.openTestsPanel()
		return
	}
	if m.activeWS().Panes.Focused() != pane.TestsKey {
		m.testsReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.TestsKey)
		return
	}
	target := m.testsReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
}

// testsPanel returns the singleton panel model, or nil when it is not open.
func (m Model) testsPanel() *testresults.Model {
	if !m.activeWS().Panes.Has(pane.TestsKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.TestsKey).Tests()
}

// openTestsPanel splits the active editor (fallback: focused leaf) at the
// adaptive placement (auxZone, #1588) with the singleton panel.
func (m *Model) openTestsPanel() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	key := m.activeWS().Panes.AddTests()
	if !m.insertToolPane(key, target, m.auxZone(target)) {
		m.activeWS().Panes.Close(key)
		return
	}
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

package app

import (
	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/lspdoctor"
	"ike/internal/pane"
)

// lsp_doctor.go wires the LSP Doctor tool window (#2164): a singleton pane
// that diagnoses language-server failures with a per-server check chain
// (binary resolution incl. the GUI-PATH gap #1614, executable/architecture
// sanity, --version, spawn + initialize round-trip) and maps the evidence to
// a concrete fix. The app owns the report (lspDoctorReport) so results and
// the previous run's failure classes survive the panel being closed — a
// re-run ('r') verifies the fix and marks each server resolved / still
// failing. The plugin's lsp.doctor command delivers the effective server
// specs via ilsp.DoctorMsg; failure toasts name the command (the established
// routing convention), so the notification leads here.

// handleLSPDoctor runs lsp.doctor: store the delivered server specs, then the
// toggle state machine mirroring toggleDoctorPanel — with the twist that
// opening or focusing the panel also starts a fresh check run.
func (m *Model) handleLSPDoctor(msg ilsp.DoctorMsg) tea.Cmd {
	if len(msg.Servers) > 0 {
		m.lspDoctorReport.SetServers(msg.Servers)
	}
	if !m.activeWS().Panes.Has(pane.LSPDoctorKey) {
		m.lspDoctorReturnFocus = m.activeWS().Panes.Focused()
		m.openLSPDoctorPanel()
		return m.runLSPDoctor()
	}
	if m.activeWS().Panes.Focused() != pane.LSPDoctorKey {
		m.lspDoctorReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.LSPDoctorKey)
		return m.runLSPDoctor()
	}
	target := m.lspDoctorReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
	return nil
}

// lspDoctorPanel returns the singleton panel model, or nil when it is not open.
func (m Model) lspDoctorPanel() *lspdoctor.Model {
	if !m.activeWS().Panes.Has(pane.LSPDoctorKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.LSPDoctorKey).LSPDoctor()
}

// openLSPDoctorPanel splits the active editor (fallback: focused leaf) at the
// adaptive placement (auxZone, #1588) with the singleton panel, sharing the
// app-owned report.
func (m *Model) openLSPDoctorPanel() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	key := m.activeWS().Panes.AddLSPDoctor()
	if !m.insertToolPane(key, target, m.auxZone(target)) {
		m.activeWS().Panes.Close(key)
		return
	}
	m.wireLSPDoctorPanel(m.activeWS().Panes.Get(key).LSPDoctor())
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// wireLSPDoctorPanel shares the app-owned report with a panel instance;
// layout restore uses it too.
func (m *Model) wireLSPDoctorPanel(p *lspdoctor.Model) {
	p.SetReport(m.lspDoctorReport)
}

// runLSPDoctor starts one check run over the stored server set, off the
// Update loop. The ResultsMsg it produces lands in Finish, which also
// computes the resolved/unresolved verdicts against the previous run.
func (m *Model) runLSPDoctor() tea.Cmd {
	if m.lspDoctorReport.Running() || len(m.lspDoctorReport.Servers()) == 0 {
		return nil
	}
	m.lspDoctorReport.Begin()
	servers := m.lspDoctorReport.Servers()
	return func() tea.Msg {
		return lspdoctor.ResultsMsg{Results: lspdoctor.Run(servers, lspdoctor.RealProbes())}
	}
}

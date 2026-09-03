package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
)

// vcs_panel.go wires the VCS tool window (Roadmap 0330, #482): a singleton
// bottom-split pane mirroring the terminal's toggle state machine —
// vcs.panel opens it below the active editor, re-toggling returns focus to
// where it came from.

// VCSPanelToggleMsg runs vcs.panel.
type VCSPanelToggleMsg struct{}

// toggleVCSPanel is the vcs.panel state machine, mirroring toggleTerminal:
// no panel → open at the bottom; unfocused → focus it; focused → return
// focus to the remembered pane.
func (m *Model) toggleVCSPanel() {
	if m.vcs.snap == nil {
		m.host.Notify(host.Info, "not a git repository")
		return
	}
	m.togglePanel(pane.VCSKey, func() tea.Cmd { m.openVCSPanel(); return nil })
}

// openVCSPanel splits the active editor (fallback: focused leaf) at the
// adaptive placement (auxZone, #1588) with the singleton panel, seeded from
// the current snapshot.
func (m *Model) openVCSPanel() {
	m.openToolPane(m.activeWS().Panes.AddVCS, m.auxZone, func(key string) {
		m.activeWS().Panes.Get(key).VCS().SetVCS(m.vcs.snap)
	})
}

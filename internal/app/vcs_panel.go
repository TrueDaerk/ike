package app

import (
	"ike/internal/host"
	"ike/internal/layout"
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
	if !m.activeWS().Panes.Has(pane.VCSKey) {
		m.vcsReturnFocus = m.activeWS().Panes.Focused()
		m.openVCSPanel()
		return
	}
	if m.activeWS().Panes.Focused() != pane.VCSKey {
		m.vcsReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.VCSKey)
		return
	}
	target := m.vcsReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
}

// openVCSPanel splits the active editor (fallback: focused leaf) at the
// bottom with the singleton panel, seeded from the current snapshot.
func (m *Model) openVCSPanel() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	key := m.activeWS().Panes.AddVCS()
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneBottom)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.activeWS().Panes.Get(key).VCS().SetVCS(m.vcs.snap)
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

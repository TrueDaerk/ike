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
	if !m.panes.Has(pane.VCSKey) {
		m.vcsReturnFocus = m.panes.Focused()
		m.openVCSPanel()
		return
	}
	if m.panes.Focused() != pane.VCSKey {
		m.vcsReturnFocus = m.panes.Focused()
		m.setFocus(pane.VCSKey)
		return
	}
	target := m.vcsReturnFocus
	if target == "" || !m.panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
}

// openVCSPanel splits the active editor (fallback: focused leaf) at the
// bottom with the singleton panel, seeded from the current snapshot.
func (m *Model) openVCSPanel() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.panes.Focused()
	}
	if target == "" || m.tree == nil {
		return
	}
	key := m.panes.AddVCS()
	tree, ok := layout.SplitLeaf(m.tree, target, key, layout.ZoneBottom)
	if !ok {
		m.panes.Close(key)
		return
	}
	m.tree = tree
	m.panes.Get(key).VCS().SetDraft(m.vcs.draft)
	m.panes.Get(key).VCS().SetVCS(m.vcs.snap)
	m.setFocus(key)
	m.layout()
	saveLayout(m.tree, m.panes)
}

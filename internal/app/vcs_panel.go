package app

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/vcs"
	"ike/internal/vcspanel"
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

// vcsPanel returns the singleton panel model, or nil when it is not open.
func (m Model) vcsPanel() *vcspanel.Model {
	if !m.activeWS().Panes.Has(pane.VCSKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.VCSKey).VCS()
}

// vcsPanelLogReload refreshes the panel's log after a mutating command
// (commit/update/checkout); a closed panel or never-opened log stays lazy.
func (m Model) vcsPanelLogReload() tea.Cmd {
	if p := m.vcsPanel(); p != nil {
		return p.ReloadLog()
	}
	return nil
}

// openCommitDiffPane splits the editor area with one commit file's diff
// against the commit's parent (0330, #484). The editor area is the target —
// splitting the focused leaf would carve a sliver out of the bottom tool
// window the request came from (#489).
func (m *Model) openCommitDiffPane(msg vcs.FileAtMsg) {
	// The same commit file re-opens by focusing the existing pane (#509);
	// revision contents are immutable, no refresh needed.
	absPath := msg.Path
	if snap := m.vcs.snap; snap != nil {
		absPath = filepath.Join(snap.Root, filepath.FromSlash(msg.Path))
	}
	if key, ok := m.findDiffPane("", absPath, msg.Hash+"^", msg.Hash); ok {
		m.setFocus(key)
		return
	}
	short := msg.Hash
	if len(short) > 7 {
		short = short[:7]
	}
	name := filepath.Base(msg.Path)
	// Single diff window (#513): retarget the existing pane.
	if key, ok := m.diffSlot(); ok {
		inst := m.activeWS().Panes.Get(key)
		inst.StopDiffEdit()
		inst.Diff().Retarget(name+" @ "+short+"^", name+" @ "+short, "", absPath, msg.Hash+"^", msg.Hash, false)
		inst.Diff().SetContents(msg.Parent, msg.Content)
		m.setFocus(key)
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
		return
	}
	key := m.activeWS().Panes.AddDiffTitled(name+" @ "+short+"^", name+" @ "+short, absPath)
	m.activeWS().Panes.Get(key).Diff().SetRevs(msg.Hash+"^", msg.Hash)
	if !m.placeDiffLeaf(key) {
		return
	}
	m.activeWS().Panes.Get(key).Diff().SetContents(msg.Parent, msg.Content)
	m.setFocus(key)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
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
	m.activeWS().Panes.Get(key).VCS().SetDraft(m.vcs.draft)
	m.activeWS().Panes.Get(key).VCS().SetVCS(m.vcs.snap)
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

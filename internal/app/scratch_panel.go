package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/scratch"
	"ike/internal/scratchpanel"
)

// scratch_panel.go wires the Scratch Files tool window (#1932): a slim
// singleton pane below the editor listing the scratch store newest-first, with
// enter/double-click opening through the standard funnel and a confirmed
// delete that removes the file and closes its open tabs. It is a normal
// split-tree tool pane, so the divider above it is the ordinary mouse-
// draggable pane edge and its height persists with the layout; [scratch]
// panel / panel_height decide whether it opens on start and how tall it opens.

// ScratchPanelToggleMsg runs scratch.panel.
type ScratchPanelToggleMsg struct{}

// toggleScratchPanel is the scratch.panel state machine, mirroring
// toggleProblemsPanel: no panel → open at the bottom; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleScratchPanel() {
	if !m.activeWS().Panes.Has(pane.ScratchKey) {
		m.scratchReturnFocus = m.activeWS().Panes.Focused()
		m.openScratchPanel()
		return
	}
	if m.activeWS().Panes.Focused() != pane.ScratchKey {
		m.scratchReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.ScratchKey)
		return
	}
	target := m.scratchReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
}

// scratchPanel returns the singleton panel model, or nil when it is not open.
func (m Model) scratchPanel() *scratchpanel.Model {
	if !m.activeWS().Panes.Has(pane.ScratchKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.ScratchKey).Scratch()
}

// openScratchPanel splits the active editor (fallback: focused leaf) at the
// bottom with the singleton panel, sized to the configured height. Unlike the
// adaptive tool windows (#1588) it always docks below: the list is a wide,
// few-rows strip, not a column.
func (m *Model) openScratchPanel() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	region := m.lay.Panes[target] // pre-split geometry: the new split's region
	key := m.activeWS().Panes.AddScratch()
	if !m.insertToolPane(key, target, layout.ZoneBottom) {
		m.activeWS().Panes.Close(key)
		return
	}
	m.sizeScratchPanel(region.H)
	m.refreshScratchPanel() // the store may have moved since the pane was built
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// sizeScratchPanel gives a freshly inserted panel the configured height out of
// the hostH rows its split region owns. Later drags of the divider own the
// ratio from then on — this only decides how tall the panel opens. A host
// height of zero (no layout computed yet, or a slot template placed the pane
// elsewhere) leaves the split at its default ratio.
func (m *Model) sizeScratchPanel(hostH int) {
	split := scratchSplit(m.activeWS().Tree)
	if split == nil || hostH <= 0 {
		return
	}
	ratio := float64(hostH-scratchPanelHeight()) / float64(hostH)
	if ratio < 0.05 {
		ratio = 0.05
	}
	if ratio > 0.95 {
		ratio = 0.95
	}
	split.Ratio = ratio
}

// scratchSplit finds the split holding the scratch panel as its bottom child —
// the one whose ratio decides the panel's height.
func scratchSplit(n layout.Node) *layout.Split {
	s, ok := n.(*layout.Split)
	if !ok {
		return nil
	}
	if leaf, ok := s.B.(*layout.Leaf); ok && leaf.Pane == pane.ScratchKey && s.Orient == layout.Vertical {
		return s
	}
	if found := scratchSplit(s.A); found != nil {
		return found
	}
	return scratchSplit(s.B)
}

// scratchPanelHeight is the configured panel height in terminal rows, chrome
// included; the settings form bounds it, so no clamping is needed here.
func scratchPanelHeight() int { return config.Get().Scratch.PanelHeight }

// refreshScratchPanel re-reads the store into an open panel; a closed panel
// costs nothing. Called whenever a scratch is created or deleted, so the list
// is current without a restart.
func (m *Model) refreshScratchPanel() {
	if p := m.scratchPanel(); p != nil {
		p.Refresh()
	}
}

// deleteScratch runs the panel's confirmed delete: the store removes the file
// (refusing anything outside the scratch dir), every editor still showing it
// closes across panes — the explorer's delete path — and the list refreshes.
func (m *Model) deleteScratch(path string) tea.Cmd {
	if err := scratch.Delete(path); err != nil {
		m.host.Notify(host.Warn, "scratch: "+err.Error())
		m.refreshScratchPanel()
		return nil
	}
	m.closeEditorsForPath(path, false)
	m.probStore.Drop(path, false)
	m.dropRawDiags(path, false)
	m.refreshProblemsPanel()
	m.refreshScratchPanel()
	m.host.Notify(host.Info, "deleted "+displayPath(path))
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return nil
}

// scratchAutoOpen honours [scratch] panel = true once per model, as soon as
// the first layout pass gave the panes real geometry: the panel opens without
// stealing focus. Toggling it away afterwards is a session decision the
// setting no longer overrides.
func (m *Model) scratchAutoOpen() {
	if m.scratchAutoDone || m.width == 0 || m.activeWS().Tree == nil {
		return
	}
	m.scratchAutoDone = true
	if !config.Get().Scratch.Panel || m.activeWS().Panes.Has(pane.ScratchKey) {
		return
	}
	focus := m.activeWS().Panes.Focused()
	m.openScratchPanel()
	if focus != "" && m.activeWS().Panes.Has(focus) {
		m.setFocus(focus)
	}
}

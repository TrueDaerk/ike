package app

import (
	"ike/internal/layout"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/usages"
)

// usages_panel.go wires the Usages tool window (#1155): a singleton
// bottom-split pane holding the latest panel-targeted find-references
// results, mirroring the Problems panel's toggle state machine (#1024). The
// LSP bridge delivers a UsagesMsg (lsp.referencesPanel); the handler opens
// the pane if needed and fills it.

// UsagesToggleMsg runs usages.toggle.
type UsagesToggleMsg struct{}

// toggleUsagesPanel is the usages.toggle state machine, mirroring
// toggleProblemsPanel: no panel → open at the bottom; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleUsagesPanel() {
	if !m.activeWS().Panes.Has(pane.UsagesKey) {
		m.usagesReturnFocus = m.activeWS().Panes.Focused()
		m.openUsagesPanel()
		return
	}
	if m.activeWS().Panes.Focused() != pane.UsagesKey {
		m.usagesReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.UsagesKey)
		return
	}
	target := m.usagesReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
}

// usagesPanel returns the singleton panel model, or nil when it is not open.
func (m Model) usagesPanel() *usages.Model {
	if !m.activeWS().Panes.Has(pane.UsagesKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.UsagesKey).Usages()
}

// openUsagesPanel splits the active editor (fallback: focused leaf) at the
// bottom with the singleton panel.
func (m *Model) openUsagesPanel() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	key := m.activeWS().Panes.AddUsages()
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneBottom)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.activeWS().Panes.Get(key).Usages().SetDisplayPath(displayPath)
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// fillUsagesPanel routes one panel-targeted find-references result (#1155)
// into the pane, opening it first when it is not part of the layout. The pane
// takes focus like JetBrains' Find Usages tool window.
func (m *Model) fillUsagesPanel(msg ilsp.UsagesMsg) {
	if m.usagesPanel() == nil {
		m.usagesReturnFocus = m.activeWS().Panes.Focused()
		m.openUsagesPanel()
	}
	p := m.usagesPanel()
	if p == nil {
		return
	}
	p.Set(msg.Symbol, msg.Refs, msg.Refresh)
	m.setFocus(pane.UsagesKey)
}

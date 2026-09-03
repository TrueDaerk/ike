package app

import (
	tea "charm.land/bubbletea/v2"

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
	m.togglePanel(pane.UsagesKey, func() tea.Cmd { m.openUsagesPanel(); return nil })
}

// usagesPanel returns the singleton panel model, or nil when it is not open.
func (m Model) usagesPanel() *usages.Model {
	if !m.activeWS().Panes.Has(pane.UsagesKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.UsagesKey).Usages()
}

// openUsagesPanel splits the active editor (fallback: focused leaf) at the
// adaptive placement (auxZone, #1588) with the singleton panel.
func (m *Model) openUsagesPanel() {
	m.openToolPane(m.activeWS().Panes.AddUsages, m.auxZone, func(key string) {
		m.activeWS().Panes.Get(key).Usages().SetDisplayPath(displayPath)
	})
}

// fillUsagesPanel routes one panel-targeted find-references result (#1155)
// into the pane, opening it first when it is not part of the layout. The pane
// takes focus like JetBrains' Find Usages tool window.
func (m *Model) fillUsagesPanel(msg ilsp.UsagesMsg) {
	m.ensurePanel(pane.UsagesKey, func() tea.Cmd { m.openUsagesPanel(); return nil })
	p := m.usagesPanel()
	if p == nil {
		return
	}
	p.Set(msg.Symbol, msg.Refs, msg.Refresh)
	m.setFocus(pane.UsagesKey)
}

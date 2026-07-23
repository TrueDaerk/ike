package app

import (
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/problems"
)

// problems_panel.go wires the Problems tool window (#1024, part of #33): a
// singleton bottom-split pane aggregating LSP diagnostics project-wide,
// mirroring the VCS panel's toggle state machine. The app-level store
// (m.probStore) is fed from every DiagnosticsMsg — including files no editor
// has open — so the pane is a pure consumer of the existing publish flow.

// ProblemsToggleMsg runs problems.toggle.
type ProblemsToggleMsg struct{}

// toggleProblemsPanel is the problems.toggle state machine, mirroring
// toggleVCSPanel: no panel → open at the bottom; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleProblemsPanel() {
	if !m.activeWS().Panes.Has(pane.ProblemsKey) {
		m.problemsReturnFocus = m.activeWS().Panes.Focused()
		m.openProblemsPanel()
		return
	}
	if m.activeWS().Panes.Focused() != pane.ProblemsKey {
		m.problemsReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.ProblemsKey)
		return
	}
	target := m.problemsReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
}

// problemsPanel returns the singleton panel model, or nil when it is not open.
func (m Model) problemsPanel() *problems.Model {
	if !m.activeWS().Panes.Has(pane.ProblemsKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.ProblemsKey).Problems()
}

// openProblemsPanel splits the active editor (fallback: focused leaf) at the
// bottom with the singleton panel, seeded from the live diagnostics store.
func (m *Model) openProblemsPanel() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	key := m.activeWS().Panes.AddProblems()
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneBottom)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	p := m.activeWS().Panes.Get(key).Problems()
	p.SetDisplayPath(displayPath)
	p.SetStore(m.probStore)
	m.syncProblemsActive()
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// refreshProblemsPanel re-derives an open panel's rows after a store update;
// a closed panel costs nothing.
func (m *Model) refreshProblemsPanel() {
	if p := m.problemsPanel(); p != nil {
		p.Refresh()
	}
}

// syncProblemsActive tracks the active editor's file into the panel for its
// current-file scope; called on focus and tab changes like the explorer's
// active-file accent.
func (m *Model) syncProblemsActive() {
	p := m.problemsPanel()
	if p == nil {
		return
	}
	path := ""
	if ed := m.activeEditor(); ed != nil && ed.HasFile() {
		path = ed.Path()
	}
	p.SetActivePath(path)
}

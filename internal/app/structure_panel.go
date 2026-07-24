package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/layout"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/structpanel"
)

// structure_panel.go wires the Structure tool window (#1025): a singleton
// right-split pane showing the focused buffer's LSP documentSymbol tree,
// mirroring the VCS panel's toggle state machine. The refresh runs through the
// registry command lsp.documentSymbols — the root model never touches the LSP
// manager — and re-fires when the pane opens, the focused buffer changes
// (structureSyncCmd in the Update wrapper) or the buffer saves.

// StructureToggleMsg runs structure.toggle.
type StructureToggleMsg struct{}

// toggleStructurePanel is the structure.toggle state machine, mirroring
// toggleVCSPanel: no panel → open at the right; unfocused → focus it;
// focused → return focus to the remembered pane.
func (m *Model) toggleStructurePanel() {
	if !m.activeWS().Panes.Has(pane.StructureKey) {
		m.structReturnFocus = m.activeWS().Panes.Focused()
		m.openStructurePanel()
		return
	}
	if m.activeWS().Panes.Focused() != pane.StructureKey {
		m.structReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(pane.StructureKey)
		return
	}
	target := m.structReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
}

// structPanel returns the singleton panel model, or nil when it is not open.
func (m Model) structPanel() *structpanel.Model {
	if !m.activeWS().Panes.Has(pane.StructureKey) {
		return nil
	}
	return m.activeWS().Panes.Get(pane.StructureKey).Structure()
}

// openStructurePanel splits the active editor (fallback: focused leaf) at the
// right with the singleton panel; the first refresh fills it.
func (m *Model) openStructurePanel() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	key := m.activeWS().Panes.AddStructure()
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneRight)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.structReqPath = "" // a fresh open always refreshes
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// structureSyncCmd runs once per settled Update pass (the Update wrapper):
// while the panel is open it follows the active editor's cursor (enclosing
// symbol highlight) and issues a documentSymbol refresh when the shown tree
// belongs to another file — or unconditionally after a save (structForce).
// The request dedup (structReqPath) keeps a provider-less file from
// re-requesting every pass. The breadcrumbs bar (#1153) shares the funnel:
// with the panel closed it still issues the request when its per-path cache
// (docSymbols) lacks the active buffer's tree.
func (m *Model) structureSyncCmd() tea.Cmd {
	sp := m.structPanel()
	if sp == nil && !m.breadcrumbsOn() {
		return nil
	}
	key := m.activeEditorKey()
	if key == "" {
		return nil
	}
	ed := m.activeWS().Panes.Get(key).Editor()
	if ed == nil || !ed.HasFile() {
		return nil
	}
	path := ed.Path()
	if sp != nil && sp.Path() == path {
		line, _ := ed.Cursor() // 1-based
		sp.Follow(line - 1)
	}
	// "Shown" data for the dedup: the open panel's tree, else the breadcrumb
	// cache entry (present even for empty / provider-less replies, so those
	// files don't re-request every pass).
	shown := ""
	if sp != nil {
		shown = sp.Path()
	} else if _, ok := m.docSymbols[path]; ok {
		shown = path
	}
	if m.structureNeedsRequest(shown, path) {
		m.structForce = false
		m.structReqPath = path
		return m.RunCommand("lsp.documentSymbols")
	}
	return nil
}

// structureNeedsRequest decides whether a refresh must be issued for the
// active editor's path: always after a save (structForce), otherwise only
// when the shown tree belongs to another file and no request for the path is
// already outstanding (a provider-less file must not re-request every pass).
func (m *Model) structureNeedsRequest(shown, path string) bool {
	if m.structForce {
		return true
	}
	return shown != path && m.structReqPath != path
}

// applyDocumentSymbols stores a documentSymbol reply in the breadcrumbs'
// per-path cache (#1153) — empty and provider-less replies included, so the
// sync's dedup sees the path as answered — and feeds the open Structure
// panel.
func (m *Model) applyDocumentSymbols(msg ilsp.DocumentSymbolsMsg) {
	if m.docSymbols == nil {
		m.docSymbols = map[string][]ilsp.SymbolNode{}
	}
	m.docSymbols[msg.Path] = msg.Symbols
	if sp := m.structPanel(); sp != nil {
		sp.SetSymbols(msg.Path, msg.Symbols, msg.NoProvider)
	}
}

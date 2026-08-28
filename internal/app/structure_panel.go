package app

import (
	"time"

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
// (structureSyncCmd in the Update wrapper) or the buffer saves. Two more
// consumers share the cache and the funnel: the breadcrumbs bar (#1153) and
// sticky scroll's symbol fallback (#2167, symbolscopes.go).

// StructureToggleMsg runs structure.toggle.
type StructureToggleMsg struct{}

// docSymEntry is one file's cached documentSymbol reply (#2319): the tree
// plus the buffer DocVersion the request was issued at, so the settled-pass
// sync can tell a still-valid tree from one the user has edited past. A
// provider-less reply caches too (NoProvider) and stays fresh regardless of
// version — re-asking a server without a documentSymbolProvider after every
// edit could never produce a tree.
type docSymEntry struct {
	Symbols    []ilsp.SymbolNode
	NoProvider bool
	Version    int
}

// structDebounceDelay is how long a symbol refresh waits before dispatching
// (#2319): rapid tab-cycling or a typing burst re-arms the timer each pass,
// so only the buffer the user settles on reaches the language server.
const structDebounceDelay = 250 * time.Millisecond

// structDebounceMsg fires a debounced documentSymbol refresh; a stale seq
// (a newer target was armed since) is dropped.
type structDebounceMsg struct{ seq int }

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
	if !m.insertToolPane(key, target, layout.ZoneRight) {
		m.activeWS().Panes.Close(key)
		return
	}
	// A fresh open always refills — from the cache when the buffer is
	// unchanged, else via an immediate (undebounced) request.
	m.structReqPath = ""
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// structureSyncCmd runs once per settled Update pass (the Update wrapper):
// while the panel is open it follows the active editor's cursor (enclosing
// symbol highlight) and keeps the shown tree on the active buffer. A switch
// to a buffer whose cached tree is still valid (docSymbols entry at the
// current DocVersion, #2319) refeeds the panel from the cache — no server
// round trip. Only a missing or edited-past tree arms the debounced refresh;
// a save (structForce) dispatches immediately, as does the first request
// after the panel opens (structReqPath == ""). The breadcrumbs bar (#1153)
// shares the funnel: with the panel closed it still requests when the
// per-path cache lacks the active buffer's tree; sticky scroll's symbol
// fallback (#2167) keeps the funnel alive with breadcrumbs off too.
func (m *Model) structureSyncCmd() tea.Cmd {
	sp := m.structPanel()
	if sp == nil && !m.breadcrumbsOn() && !m.stickySymbolsOn() {
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
	path, version := ed.Path(), ed.DocVersion()
	if sp != nil && sp.Path() == path {
		line, _ := ed.Cursor() // 1-based
		sp.Follow(line - 1)
	}
	entry, cached := m.docSymbols[path]
	fresh := cached && (entry.NoProvider || entry.Version == version)
	// Cache hit on a buffer switch: refeed the panel without a request.
	if sp != nil && sp.Path() != path && fresh && !m.structForce {
		sp.SetSymbols(path, entry.Symbols, entry.NoProvider)
	}
	switch {
	case m.structForce, m.structReqPath == "" && !fresh:
		// A save must re-ask even for an unchanged version, and the first
		// request after the panel opens skips the debounce — the user is
		// waiting on an empty pane.
		m.structForce = false
		return m.structureDispatch(path, version)
	case fresh:
		return nil
	case m.structReqPath == path && m.structReqVersion == version:
		return nil // request already outstanding for exactly this state
	case m.structDebPath == path && m.structDebVersion == version:
		return nil // debounce already armed for exactly this state
	}
	// Arm (or re-arm) the debounce: a newer target invalidates older ticks
	// via the seq, so rapid tab-cycling or typing sends one request at rest.
	m.structDebPath, m.structDebVersion = path, version
	m.structDebSeq++
	seq := m.structDebSeq
	return tea.Tick(structDebounceDelay, func(time.Time) tea.Msg {
		return structDebounceMsg{seq: seq}
	})
}

// structureDispatch records the request target (path + DocVersion, the dedup
// and the version stamp for the async reply) and issues the registry command.
func (m *Model) structureDispatch(path string, version int) tea.Cmd {
	m.structDebPath = "" // a direct dispatch cancels any armed debounce
	m.structReqPath = path
	m.structReqVersion = version
	return m.RunCommand("lsp.documentSymbols")
}

// structureDebounceFire handles an elapsed debounce timer: a stale seq (a
// newer target was armed) is dropped, as is a tick whose target no longer
// matches the active buffer's path and version — the settled pass re-arms
// for whatever the user settled on.
func (m *Model) structureDebounceFire(msg structDebounceMsg) tea.Cmd {
	if msg.seq != m.structDebSeq || m.structDebPath == "" {
		return nil
	}
	key := m.activeEditorKey()
	if key == "" {
		m.structDebPath = ""
		return nil
	}
	ed := m.activeWS().Panes.Get(key).Editor()
	if ed == nil || !ed.HasFile() || ed.Path() != m.structDebPath || ed.DocVersion() != m.structDebVersion {
		m.structDebPath = ""
		return nil
	}
	return m.structureDispatch(ed.Path(), ed.DocVersion())
}

// applyDocumentSymbols stores a documentSymbol reply in the per-path cache
// (#1153/#2319) — empty and provider-less replies included, so the sync's
// dedup sees the path as answered — stamped with the DocVersion the request
// was issued at (a reply landing after further edits caches as stale and the
// next settled pass re-arms the refresh), feeds the open Structure panel and
// refreshes sticky scroll's fallback scopes (#2167).
func (m *Model) applyDocumentSymbols(msg ilsp.DocumentSymbolsMsg) {
	if m.docSymbols == nil {
		m.docSymbols = map[string]docSymEntry{}
	}
	version := 0
	if msg.Path == m.structReqPath {
		version = m.structReqVersion
	} else if ed := m.editorForPath(msg.Path); ed != nil {
		version = ed.DocVersion()
	}
	m.docSymbols[msg.Path] = docSymEntry{Symbols: msg.Symbols, NoProvider: msg.NoProvider, Version: version}
	if sp := m.structPanel(); sp != nil {
		sp.SetSymbols(msg.Path, msg.Symbols, msg.NoProvider)
	}
	// Sticky scroll's LSP fallback (#2167) rides the same delivery: the views
	// showing the file get the derived scopes now, so a post-save refresh
	// re-pins in the pass it lands in.
	m.pushSymbolScopes(msg.Path, msg.Symbols)
}

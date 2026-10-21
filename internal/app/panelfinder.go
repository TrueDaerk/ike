package app

import "ike/internal/pane"

// panelfinder.go — the "@" file finder from a tool panel (#3025). In an
// editor's normal mode "@" floats the file-only finder over the editor; the
// read-only list/view tool windows have no meaning of their own for the key,
// so it opens the same finder over the panel, and the pick lands as a tab in
// the panel's pane rather than in some other editor.

// panelAtFinder reports whether "@" opens the file finder over the focused
// tool panel: the panel is one of the list/view windows without an "@"
// binding of its own, and none of its text inputs (search line, filter row,
// prompt, picker) holds the keyboard — there "@" is typed text. Panels with
// composers or consoles (VCS, Issues, HTTP, DOM, Debug, data viewers) and the
// terminal never qualify.
func (m Model) panelAtFinder() bool {
	inst := m.focusedContent() // the panel may be a hosted tab (#2736)
	if inst == nil {
		return false
	}
	switch inst.Kind() {
	case pane.KindAgentTrace:
		return !inst.AgentTrace().PickerOpen()
	case pane.KindBreakpoints:
		return !inst.Breakpoints().Editing()
	case pane.KindComplexity, pane.KindProblems, pane.KindUsages, pane.KindDeps,
		pane.KindTime, pane.KindUsage:
		return !inst.LineInputOpen()
	case pane.KindStructure, pane.KindTests, pane.KindDoctor, pane.KindLSPDoctor:
		return true
	}
	return false
}

// openPanelFilePalette opens the anchored file-only finder over the focused
// tool panel and remembers the panel's pane as the pick's target.
func (m *Model) openPanelFilePalette() {
	m.openFilePaletteAnchored()
	m.panelFinderHost = m.activeWS().Panes.Focused()
}

// routePanelFinderPick prepares a palette.OpenFileMsg for the panel the "@"
// finder was opened from: the pane converts into a tab host in place
// (ensureTabHost — a singleton window keeps its identity as the first tab
// and the host takes a fresh key) and is focused, and the next plain file
// open is forced into it (fileTabHost). A viewer file follows the focused
// pane through openPathFocused on its own. The request is consumed either
// way; a pick that did not come from the panel's finder is left alone.
func (m *Model) routePanelFinderPick() {
	host := m.panelFinderHost
	m.panelFinderHost = ""
	if host == "" || !m.palette.Anchored() || m.activeWS().Panes.Focused() != host {
		return
	}
	key, ok := m.ensureTabHost(host)
	if !ok {
		return
	}
	m.setFocus(key)
	m.fileTabHost = key
}

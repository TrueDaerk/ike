package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/layout"
	"ike/internal/pane"
)

// panelwiring.go holds the shared wiring every tool window and viewer pane
// repeated verbatim (#2463): the toggle state machine, the open-or-focus half
// of the message-driven openers, the tool-pane split, the viewer-pane split
// and the background-result routing. Each panel file keeps only what is truly
// its own — which pane it adds, where it goes, how it is seeded — so a change
// to the shared behaviour (placement fallbacks, focus restore, layout
// persistence) happens in one place.

// setPanelReturn remembers the pane a tool window should hand focus back to
// when it is toggled off. The map is created on demand, so a Model built
// without it (tests, zero values) is safe to write to.
func (m *Model) setPanelReturn(key, target string) {
	if m.panelReturnFocus == nil {
		m.panelReturnFocus = make(map[string]string)
	}
	m.panelReturnFocus[key] = target
}

// panelReturnTarget resolves where focus goes when a tool window is toggled
// off: the pane that opened it, else the active editor, else the explorer —
// the fallback chain every toggle used to spell out for itself.
func (m *Model) panelReturnTarget(key string) string {
	target := m.panelReturnFocus[key]
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	return target
}

// togglePanel is the shared tool-window toggle state machine: no pane → open
// it (remembering the caller's focus), open but unfocused → focus it, focused
// → return focus along panelReturnTarget's chain. open runs only in the first
// case and its command is passed through, so an opener that seeds the pane
// with a background fetch keeps doing so.
func (m *Model) togglePanel(key string, open func() tea.Cmd) tea.Cmd {
	return m.togglePanelWith(key, open, nil)
}

// togglePanelWith is togglePanel with a hook for the two panels that do
// something extra when the pane is merely refocused: the issues window views
// its pending forge events, the LSP doctor starts a fresh check run.
func (m *Model) togglePanelWith(key string, open func() tea.Cmd, onFocus func() tea.Cmd) tea.Cmd {
	if !m.activeWS().Panes.Has(key) {
		m.setPanelReturn(key, m.activeWS().Panes.Focused())
		return open()
	}
	if m.activeWS().Panes.Focused() != key {
		m.setPanelReturn(key, m.activeWS().Panes.Focused())
		m.setFocus(key)
		if onFocus != nil {
			return onFocus()
		}
		return nil
	}
	m.setFocus(m.panelReturnTarget(key))
	return nil
}

// ensurePanel opens a tool window when it is not part of the layout,
// remembering the caller's focus like togglePanel does. An open pane — focused
// or not — is left exactly as it is; the message-driven openers that fill a
// pane with a result decide focus for themselves afterwards.
func (m *Model) ensurePanel(key string, open func() tea.Cmd) tea.Cmd {
	if m.activeWS().Panes.Has(key) {
		return nil
	}
	m.setPanelReturn(key, m.activeWS().Panes.Focused())
	return open()
}

// showPanel makes sure a tool window exists and is focused: ensurePanel plus
// the focus half of togglePanel, without its third (toggle-off) branch — for
// the notification paths that reveal a window rather than toggle it.
func (m *Model) showPanel(key string, open func() tea.Cmd) tea.Cmd {
	if !m.activeWS().Panes.Has(key) {
		m.setPanelReturn(key, m.activeWS().Panes.Focused())
		return open()
	}
	if m.activeWS().Panes.Focused() != key {
		m.setPanelReturn(key, m.activeWS().Panes.Focused())
		m.setFocus(key)
	}
	return nil
}

// fixedZone adapts a constant placement to openToolPane's zone seam, for the
// panels that always take the same side rather than the adaptive auxZone.
func fixedZone(z layout.Zone) func(string) layout.Zone {
	return func(string) layout.Zone { return z }
}

// openToolPane splits the active editor (fallback: the focused leaf) with a
// singleton tool window: add mints the pane, zone picks the placement for the
// chosen target, and after seeds the fresh pane before it takes focus. It
// reports whether the pane made it into the layout — a failed split closes the
// pane again and leaves the tree untouched.
func (m *Model) openToolPane(add func() string, zone func(target string) layout.Zone, after func(key string)) bool {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return false
	}
	key := add()
	if !m.insertToolPane(key, target, zone(target)) {
		m.activeWS().Panes.Close(key)
		return false
	}
	if after != nil {
		after(key)
	}
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return true
}

// openViewerPane opens (or refocuses) a viewer for id in the pane the open
// asked for — the focused pane for the palette (#1825), the last-focused
// editor for the explorer's default open (#1851) — and otherwise split off the
// leaf viewerSplitTarget picks (#1779). matches recognises an already open
// viewer for the same content, add mints the pane. The returned command is the
// pane's own Init, which is how the viewers that load in the background (#1795)
// get going without costing the IDE a frame.
func (m *Model) openViewerPane(kind pane.Kind, id string, matches func(*pane.Instance) bool, add func() string) tea.Cmd {
	tabHost := m.takeViewerTabHost()
	if hostKey, tabIdx, _, ok := m.findContent(matches); ok {
		m.focusContentAt(hostKey, tabIdx) // may live in a tab (#1778)
		return nil
	}
	if tabHost != "" {
		if nested, ok := m.openContentTab(tabHost, kind, id); ok {
			return nested.Init()
		}
	}
	return m.splitViewerPane(add)
}

// splitViewerPane is openViewerPane's tail: mint the pane, split the viewer
// target leaf to the right, focus it, persist the layout and return the pane's
// Init. The remote browser (#1997), whose dedup is by connection rather than
// by path, uses it directly.
func (m *Model) splitViewerPane(add func() string) tea.Cmd {
	target := m.viewerSplitTarget()
	key := add()
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneRight)
	if !ok {
		m.activeWS().Panes.Close(key)
		return nil
	}
	m.activeWS().Tree = tree
	m.layout()
	m.setFocus(key)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return nil
	}
	return inst.Init()
}

// routeResult delivers one background viewer result to the pane that asked for
// it — dedicated or tab-nested (#1778) — matched by the pane model's own key.
// A result whose pane is gone is discarded rather than dropped: discard is the
// message's Discard(), which releases the handle (database, connection) the
// result carries.
func (m *Model) routeResult(msg tea.Msg, match func(*pane.Instance) bool, discard func()) tea.Cmd {
	_, _, inst, ok := m.findContent(match)
	if !ok {
		discard()
		return nil
	}
	return inst.Update(msg)
}

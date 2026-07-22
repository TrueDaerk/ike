package app

import (
	"sort"
	"strings"

	"ike/internal/host"
	"ike/internal/layout"
)

// maximize.go implements pane.maximize (Roadmap 0290, #358): tmux-style zoom.
// The focused pane temporarily renders alone over the whole body rect while
// the split tree stays untouched underneath — m.lay (the single source of
// pane geometry for rendering, mouse hit-testing and focus navigation)
// becomes a one-pane layout, so no other subsystem needs a zoom branch. Any
// change to the tree's leaf set (split, close, relocation) auto-unzooms via
// the signature check in layout(); zoom is deliberately not persisted.

// toggleMaximize flips the zoom: zoomed → restore the full layout; unzoomed →
// zoom the focused pane. Zooming records the tree's leaf signature so layout()
// can detect structural changes without per-callsite guards. Unzooming also
// leaves zen mode — zen without a zoom would be a bare chrome-less layout.
func (m *Model) toggleMaximize() {
	if m.zoomed != "" {
		m.zoomed = ""
		m.zen = false
		m.layout()
		return
	}
	key := m.activeWS().Panes.Focused()
	if key == "" || m.activeWS().Tree == nil {
		return
	}
	if _, ok := m.lay.Panes[key]; !ok {
		return
	}
	m.zoomed = key
	m.zoomSig = leavesSignature(m.activeWS().Tree)
	m.layout()
}

// zoomActive reports whether the zoom is still valid for the current tree,
// clearing it when the leaf set changed or the pane vanished. Called from
// layout(), the choke point every mutation already goes through.
func (m *Model) zoomActive() bool {
	if m.zoomed == "" {
		return false
	}
	if !m.activeWS().Panes.Has(m.zoomed) || leavesSignature(m.activeWS().Tree) != m.zoomSig {
		m.zoomed = ""
		m.zen = false
		return false
	}
	return true
}

// toggleZen flips zen mode (#359): the focused pane maximized plus the tab
// bar and status line hidden — pure text, JetBrains distraction-free. Any
// pane kind qualifies (#934): editor, terminal, or tool pane. Leaving zen
// restores the chrome; the zoom stays only when that same pane was already
// manually zoomed before zen (else the full layout returns).
func (m *Model) toggleZen() {
	if m.zen {
		m.zen = false
		if !m.zenKeepZoom {
			m.zoomed = ""
		}
		m.layout()
		return
	}
	key := m.activeWS().Panes.Focused()
	if key == "" || m.activeWS().Tree == nil {
		m.host.Notify(host.Info, "zen mode needs a focused pane")
		return
	}
	if _, ok := m.lay.Panes[key]; !ok {
		return
	}
	m.zenKeepZoom = m.zoomed == key
	m.zen = true
	m.zoomed = key
	m.zoomSig = leavesSignature(m.activeWS().Tree)
	m.layout()
}

// leavesSignature renders the tree's leaf set order-independently, so a
// resize or relocation of ratios never counts as a structural change but any
// added/removed pane does.
func leavesSignature(root layout.Node) string {
	ls := layout.Leaves(root)
	sort.Strings(ls)
	return strings.Join(ls, "\x00")
}

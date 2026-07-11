package app

import (
	"sort"
	"strings"

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
// can detect structural changes without per-callsite guards.
func (m *Model) toggleMaximize() {
	if m.zoomed != "" {
		m.zoomed = ""
		m.layout()
		return
	}
	key := m.panes.Focused()
	if key == "" || m.tree == nil {
		return
	}
	if _, ok := m.lay.Panes[key]; !ok {
		return
	}
	m.zoomed = key
	m.zoomSig = leavesSignature(m.tree)
	m.layout()
}

// zoomActive reports whether the zoom is still valid for the current tree,
// clearing it when the leaf set changed or the pane vanished. Called from
// layout(), the choke point every mutation already goes through.
func (m *Model) zoomActive() bool {
	if m.zoomed == "" {
		return false
	}
	if !m.panes.Has(m.zoomed) || leavesSignature(m.tree) != m.zoomSig {
		m.zoomed = ""
		return false
	}
	return true
}

// leavesSignature renders the tree's leaf set order-independently, so a
// resize or relocation of ratios never counts as a structural change but any
// added/removed pane does.
func leavesSignature(root layout.Node) string {
	ls := layout.Leaves(root)
	sort.Strings(ls)
	return strings.Join(ls, "\x00")
}

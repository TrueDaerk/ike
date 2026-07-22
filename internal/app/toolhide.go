package app

import (
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
)

// toolhide.go implements window.hideAllTools (#791), JetBrains'
// cmd+shift+F12: the first press removes every visible tool window from the
// layout after snapshotting the full tree; the second press restores that
// tree exactly. Editor panes, splits and focus stay untouched — this
// complements zen mode (focus ONE file) by reclaiming editor space while
// keeping the split layout.

// isToolKind reports whether a pane kind counts as a tool window: the
// explorer, terminals, and the VCS/debug tool panels. Document viewers
// (editors, markdown preview, diff) are content, not tools.
func isToolKind(k pane.Kind) bool {
	switch k {
	case pane.KindExplorer, pane.KindTerminal, pane.KindVCS, pane.KindDebug:
		return true
	}
	return false
}

// toolHideSnapshot remembers what the first press removed. Instances stay
// registered (terminals keep running) — only the layout leaves go.
type toolHideSnapshot struct {
	tree   layout.Node // deep copy of the tree before hiding
	hidden []string    // tool keys removed, in leaf order
	sig    string      // leaf signature right after hiding
}

// toggleToolWindows runs one hide/restore step.
func (m *Model) toggleToolWindows() {
	if m.toolHide != nil {
		m.restoreToolWindows()
		return
	}
	m.hideToolWindows()
}

// hideToolWindows removes every visible tool leaf and stores the snapshot.
func (m *Model) hideToolWindows() {
	ws := m.activeWS()
	var tools []string
	for _, key := range layout.Leaves(ws.Tree) {
		if inst := ws.Panes.Get(key); inst != nil && isToolKind(inst.Kind()) {
			tools = append(tools, key)
		}
	}
	if len(tools) == 0 {
		m.host.Notify(host.Info, "no tool windows visible")
		return
	}
	snap := &toolHideSnapshot{tree: layout.Clone(ws.Tree)}
	focusHidden := false
	for _, key := range tools {
		tree, ok := layout.Close(ws.Tree, key)
		if !ok {
			continue // the last remaining leaf never closes
		}
		ws.Tree = tree
		snap.hidden = append(snap.hidden, key)
		if ws.Panes.Focused() == key {
			focusHidden = true
		}
	}
	if len(snap.hidden) == 0 {
		m.host.Notify(host.Info, "no tool windows visible")
		return
	}
	snap.sig = leavesSignature(ws.Tree)
	m.toolHide = snap
	if focusHidden {
		if key := m.activeEditorKey(); key != "" {
			m.setFocus(key)
		} else {
			m.setFocus(m.focusAfterClose())
		}
	}
	m.layout()
	saveLayout(ws.Tree, ws.Panes)
}

// restoreToolWindows brings the snapshot back. When the editor layout is
// unchanged since the hide (same leaf signature) and every hidden tool is
// still registered and still hidden, the saved tree comes back verbatim.
// Anything diverged — a tool re-opened manually while hidden, a tool
// instance gone, editor splits changed — falls back to re-attaching each
// still-hidden tool at its conventional side (explorer left, others bottom).
func (m *Model) restoreToolWindows() {
	snap := m.toolHide
	m.toolHide = nil
	ws := m.activeWS()
	visible := map[string]bool{}
	for _, key := range layout.Leaves(ws.Tree) {
		visible[key] = true
	}
	restorable := snap.hidden[:0]
	exact := leavesSignature(ws.Tree) == snap.sig
	for _, key := range snap.hidden {
		switch {
		case !ws.Panes.Has(key):
			exact = false // closed while hidden: prune from the saved tree
		case visible[key]:
			exact = false // re-opened manually: already back
		default:
			restorable = append(restorable, key)
		}
	}
	if len(restorable) == 0 {
		m.layout()
		return
	}
	if exact {
		ws.Tree = snap.tree
	} else {
		for _, key := range restorable {
			ws.Tree = attachToolPane(ws.Tree, key, ws.Panes.Get(key).Kind())
		}
	}
	m.layout()
	saveLayout(ws.Tree, ws.Panes)
}

// attachToolPane re-attaches a tool leaf at its conventional side: the
// explorer as the outer-left column, everything else as a bottom strip.
func attachToolPane(tree layout.Node, key string, kind pane.Kind) layout.Node {
	leaf := &layout.Leaf{Pane: key}
	if kind == pane.KindExplorer {
		return &layout.Split{Orient: layout.Horizontal, Ratio: 0.25, A: leaf, B: tree}
	}
	return &layout.Split{Orient: layout.Vertical, Ratio: 0.7, A: tree, B: leaf}
}

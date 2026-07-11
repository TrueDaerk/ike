package app

import (
	"ike/internal/layout"
	"ike/internal/pane"
)

// explorer_toggle.go implements the JetBrains cmd+1 state machine behind
// explorer.toggle (#268): a focused tree hides (editors reclaim the width),
// a visible unfocused tree gains focus, and a hidden tree comes back at its
// remembered ratio and takes focus. Only the layout leaf comes and goes —
// the pane instance stays registered, so expansion, selection and scroll
// survive a hide/show round-trip.

// toggleExplorer runs one state-machine step.
func (m *Model) toggleExplorer() {
	if !m.explorerVisible() {
		m.showExplorer()
		return
	}
	if m.panes.Focused() == pane.ExplorerKey {
		m.hideExplorer()
		return
	}
	m.setFocus(pane.ExplorerKey)
}

// explorerVisible reports whether the explorer leaf is in the layout tree.
func (m Model) explorerVisible() bool {
	for _, key := range layout.Leaves(m.tree) {
		if key == pane.ExplorerKey {
			return true
		}
	}
	return false
}

// hideExplorer removes the explorer leaf, remembering its split ratio. The
// last remaining leaf can never be removed (layout.Close refuses), so a
// workspace that is only the explorer stays as it is.
func (m *Model) hideExplorer() {
	if r, ok := explorerSplitRatio(m.tree); ok {
		m.explorerRatio = r
	}
	tree, ok := layout.Close(m.tree, pane.ExplorerKey)
	if !ok {
		return
	}
	m.tree = tree
	if m.panes.Focused() == pane.ExplorerKey {
		if key := m.activeEditorKey(); key != "" {
			m.setFocus(key)
		} else {
			m.setFocus(m.focusAfterClose())
		}
	}
	m.layout()
	saveLayout(m.tree, m.panes)
}

// showExplorer re-inserts the explorer as the outer-left split at its
// remembered ratio (the default width when none is remembered) and focuses it.
func (m *Model) showExplorer() {
	if !m.panes.Has(pane.ExplorerKey) || m.explorerVisible() {
		return
	}
	ratio := m.explorerRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = defaultExplorerRatio(m.width)
	}
	m.tree = &layout.Split{
		Orient: layout.Horizontal,
		Ratio:  ratio,
		A:      &layout.Leaf{Pane: pane.ExplorerKey},
		B:      m.tree,
	}
	m.setFocus(pane.ExplorerKey)
	m.layout()
	saveLayout(m.tree, m.panes)
}

// defaultExplorerRatio mirrors layout.Default's explorer sizing for width.
func defaultExplorerRatio(width int) float64 {
	if width <= 1 {
		return 0.3
	}
	r := float64(explorerWidth) / float64(width-1)
	if r <= 0 || r >= 1 {
		return 0.3
	}
	return r
}

// explorerSplitRatio finds the split that carries the explorer leaf as its
// left/top child and returns its ratio — the width the tree should come back
// at. ok=false when the explorer is hidden or sits in an unusual position
// (then the default width applies on show).
func explorerSplitRatio(n layout.Node) (float64, bool) {
	s, ok := n.(*layout.Split)
	if !ok {
		return 0, false
	}
	if leaf, ok := s.A.(*layout.Leaf); ok && leaf.Pane == pane.ExplorerKey {
		return s.Ratio, true
	}
	if r, ok := explorerSplitRatio(s.A); ok {
		return r, true
	}
	return explorerSplitRatio(s.B)
}

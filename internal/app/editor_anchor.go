package app

import (
	"ike/internal/layout"
	"ike/internal/pane"
)

// editor_anchor.go decides where a fresh editor lands when no live pane
// edits files (#1989, #3109), and names the flexible-region anchor every
// tool-pane open targets in that state (#3109). The reference is the
// designated default layout (or the built-in explorer+editor arrangement):
// its editor slot is the flexible region, and the live tree is matched
// against it by geometry — which live pane *stands in the slot's place* —
// rather than by splitting the slot's nearest layout neighbour, which (#3109)
// carved the editor out of the explorer's own narrow cell while a tool window
// kept the whole area the editor should have.

// editorAnchor is where spawnEditor splits a recreated editor in: a leaf
// (target) or a whole subtree (node — the hop-level split, #3109), the side
// the new editor takes, and the A-share of the resulting split (0 keeps the
// even split). An empty anchor leaves the caller its focused-pane fallback.
type editorAnchor struct {
	target string
	node   layout.Node
	zone   layout.Zone
	ratio  float64
}

func (a editorAnchor) empty() bool { return a.target == "" && a.node == nil }

// referenceLayout is the layout the flexible region is read from: the
// designated default layout, else the built-in explorer+editor arrangement.
func (m *Model) referenceLayout() (layout.Node, map[string]paneIdentity) {
	if tree, ids, ok := defaultLayoutSnapshot(); ok {
		return tree, ids
	}
	return layout.Default(m.width, explorerWidth), map[string]paneIdentity{
		pane.ExplorerKey: {Kind: "explorer"},
		"editor":         {Kind: "editor"},
	}
}

// editorSlotAnchor picks where a fresh editor pane splits in when no live
// pane edits files (#1989): a tool-tab host must not attract the split, so
// the reference layout's editor slot decides — with all editors closed,
// opening a file recreates the editor in its original layout slot. The
// resolution (#3109), in order:
//
//  1. A live pane standing in the slot's place (a tool window, a tool-tab
//     host, a viewer) is split so the editor takes the slot's area and the
//     occupant keeps a share.
//  2. Otherwise the slot's ancestor path is walked inside-out and the live
//     subtree standing for the first sibling with live counterparts is split
//     at that level, the sibling keeping the cell extent the layout gave it
//     — so the editor regains the slot's share of the row/column, never a
//     share of one neighbour's cell.
//  3. Without geometry (pre-layout) the first live counterpart leaf anchors
//     with the saved split share, the pre-#3109 rule.
//
// Without any usable anchor the explorer anchors (built-in arrangement) and
// an empty anchor leaves the caller its focused-pane fallback.
func (m *Model) editorSlotAnchor() editorAnchor {
	tree, ids := m.referenceLayout()
	if a, ok := m.anchorFromLayout(tree, ids); ok {
		return a
	}
	if m.liveLeaf(pane.ExplorerKey) {
		r := 0.0
		if m.width > 0 {
			r = float64(explorerWidth) / float64(m.width)
		}
		return editorAnchor{target: pane.ExplorerKey, zone: layout.ZoneRight, ratio: r}
	}
	return editorAnchor{zone: m.splitZone}
}

// anchorFromLayout resolves the reference layout's editor slot to a live
// split anchor (see editorSlotAnchor for the precedence).
func (m *Model) anchorFromLayout(tree layout.Node, ids map[string]paneIdentity) (editorAnchor, bool) {
	slot := editorSlotLeaf(tree, ids)
	if slot == "" {
		return editorAnchor{}, false
	}
	live := m.activeWS().Tree
	vp := m.bodyRect()
	if live != nil && vp.W > 0 && vp.H > 0 {
		saved := layout.Rects(tree, vp)
		rects := layout.Rects(live, vp)
		if occ := m.slotOccupant(tree, ids, slot, saved, rects); occ != "" {
			zone, ratio := m.occupantShare(occ, rects[occ])
			return editorAnchor{target: occ, zone: zone, ratio: ratio}, true
		}
		if a, ok := m.hopLevelAnchor(tree, ids, slot, saved, rects); ok {
			return a, true
		}
	}
	// No geometry to match against: the nearest layout neighbour with a live
	// counterpart anchors with the saved split share (the #1989 rule).
	for _, h := range layout.Hops(tree, slot) {
		for _, key := range layout.Leaves(h.Sibling) {
			if c := m.liveCounterpart(ids[key]); c != "" {
				return editorAnchor{target: c, zone: layout.Opposite(h.Zone), ratio: h.Ratio}, true
			}
		}
	}
	return editorAnchor{}, false
}

// slotOccupant names the live leaf standing in the editor slot's place:
// a leaf no other saved leaf accounts for (not a live counterpart of a
// non-slot identity, never the explorer) whose cell lies at least half
// inside the slot's saved rectangle — the one covering the most of it when
// several do. "" when the slot's area is held by accounted neighbours only.
func (m *Model) slotOccupant(tree layout.Node, ids map[string]paneIdentity, slot string, saved, rects map[string]layout.Rect) string {
	slotRect, ok := saved[slot]
	if !ok || slotRect.W <= 0 || slotRect.H <= 0 {
		return ""
	}
	accounted := map[string]bool{pane.ExplorerKey: true}
	for _, key := range layout.Leaves(tree) {
		if key == slot {
			continue
		}
		if c := m.liveCounterpart(ids[key]); c != "" {
			accounted[c] = true
		}
	}
	best, bestArea := "", 0
	for _, key := range layout.Leaves(m.activeWS().Tree) {
		if accounted[key] || m.activeWS().Panes.Get(key) == nil {
			continue
		}
		r := rects[key]
		area := layout.Overlap(r, slotRect)
		if area <= 0 || area*2 < r.W*r.H {
			continue
		}
		if area > bestArea {
			best, bestArea = key, area
		}
	}
	return best
}

// occupantShare decides how a recreated editor shares the slot with its
// occupant: a flexible-region pane (a viewer) splits evenly along the
// configured split orientation; a tool window keeps the auxiliary share
// (toolDockShare) on its adaptive side — below, or to the right of a wide
// cell — and the editor takes the rest.
func (m *Model) occupantShare(occ string, r layout.Rect) (layout.Zone, float64) {
	if flexPane(m.activeWS().Panes.Get(occ)) {
		return m.splitZone, 0
	}
	if r.W > auxSplitRightMinW && r.W > r.H {
		return layout.ZoneLeft, 1 - toolDockShare
	}
	return layout.ZoneTop, 1 - toolDockShare
}

// hopLevelAnchor walks the slot's ancestor path inside-out and, for the
// first sibling whose leaves have live counterparts, splits the live subtree
// those counterparts span (their common ancestor) on the slot's side. The
// sibling keeps the cell extent the layout gave it along the split axis (so
// the explorer is exactly as wide as the layout says, never a share of its
// own cell), and a sibling with no room left for the editor (its live
// subtree is no bigger than its saved cell) yields to the next hop.
func (m *Model) hopLevelAnchor(tree layout.Node, ids map[string]paneIdentity, slot string, saved, rects map[string]layout.Rect) (editorAnchor, bool) {
	live := m.activeWS().Tree
	for _, h := range layout.Hops(tree, slot) {
		set := map[string]bool{}
		for _, key := range layout.Leaves(h.Sibling) {
			if c := m.liveCounterpart(ids[key]); c != "" {
				set[c] = true
			}
		}
		node := layout.CommonAncestor(live, set)
		if node == nil {
			continue
		}
		savedRect, ok1 := layout.Bounds(h.Sibling, saved)
		liveRect, ok2 := layout.Bounds(node, rects)
		if !ok1 || !ok2 {
			continue
		}
		horizontal := h.Zone == layout.ZoneLeft || h.Zone == layout.ZoneRight
		want, have := savedRect.H, liveRect.H
		if horizontal {
			want, have = savedRect.W, liveRect.W
		}
		if have <= 0 {
			continue
		}
		share := float64(want) / float64(have)
		if share >= 0.95 {
			continue // no room for the editor beside this sibling
		}
		ratio := share // sibling is A (left/top of the new split)
		if h.Zone == layout.ZoneRight || h.Zone == layout.ZoneBottom {
			ratio = 1 - share
		}
		return editorAnchor{node: node, zone: layout.Opposite(h.Zone), ratio: ratio}, true
	}
	return editorAnchor{}, false
}

// flexAnchorKey names the pane a tool-pane open hangs off (#3109): the
// active editor; else a live flexible-region pane (the MRU viewer, #2507);
// else the live pane standing in the reference layout's editor slot — a tool
// window or tool host occupying the editor area while no editor is open;
// "" when nothing stands for the flexible region, which leaves the caller
// its focused-pane fallback. Slot templates (#1897) and explicit home
// positions (#1889) are resolved by the callers before this anchor matters.
func (m *Model) flexAnchorKey() string {
	if key := m.activeEditorKey(); key != "" {
		return key
	}
	if m.recentFlex != "" && m.liveLeaf(m.recentFlex) && flexPane(m.activeWS().Panes.Get(m.recentFlex)) {
		return m.recentFlex
	}
	for _, key := range m.leafOrder() {
		if flexPane(m.activeWS().Panes.Get(key)) {
			return key
		}
	}
	return m.liveSlotOccupant()
}

// liveSlotOccupant is slotOccupant over the reference layout and the live
// geometry, "" without either.
func (m *Model) liveSlotOccupant() string {
	live := m.activeWS().Tree
	vp := m.bodyRect()
	if live == nil || vp.W <= 0 || vp.H <= 0 {
		return ""
	}
	tree, ids := m.referenceLayout()
	slot := editorSlotLeaf(tree, ids)
	if slot == "" {
		return ""
	}
	return m.slotOccupant(tree, ids, slot, layout.Rects(tree, vp), layout.Rects(live, vp))
}

// toolAnchorKey is the target every tool-pane open splits off when no slot
// template or home position decides: the flexible-region anchor, else the
// focused leaf.
func (m *Model) toolAnchorKey() string {
	if key := m.flexAnchorKey(); key != "" {
		return key
	}
	return m.activeWS().Panes.Focused()
}

// noLiveEditorFlexAnchor reports the flexible-region stand-in a home-edge
// dock yields to (#3109): with no editor open, a tool whose home is only the
// default edge (the Run tool's `bottom`) opens in the flexible region instead
// of beside whatever tool strip the focus sits in. "" when an editor is live
// or nothing stands in the editor slot — the home dock applies as before.
func (m *Model) noLiveEditorFlexAnchor() string {
	if m.activeEditorKey() != "" {
		return ""
	}
	return m.flexAnchorKey()
}

package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"ike/internal/layout"
)

// runhome.go remembers where the user put the Run tool (#2191). For an
// ordinary tool the configured placement is intent, never state — a moved
// tool returns to its home on the next open (#1889). Run output is different:
// it reappears on every run, so once the user has dragged it somewhere the
// next run belongs *there*, not back at the configured edge. The position is
// per-project state like the layout itself, so it survives a restart, and it
// survives the pane's close — the layout's `runTool` leaf prunes on restore
// (a program must not re-run itself at startup), which would otherwise take
// the remembered position with it.
//
// The record is written only when a layout drag actually moves the Run pane,
// so a placement that was never overruled keeps following the setting; it
// carries the placement it overruled, so changing `run.placement` re-asserts
// the setting instead of being shadowed forever by a stale drag.

// runHome is the remembered Run tool position: the live pane it hangs off,
// the side it sits on, and the parent split's share. Tab means the Run tool
// lived as a tab of the anchor pane rather than beside it; Root means it hung
// straight off the root split — a full-span workspace dock (#811), which
// re-docks as one instead of splitting a single neighbour, and whose Ratio is
// the Run pane's own share of the workspace.
type runHome struct {
	Anchor    string  `json:"anchor"`
	Zone      string  `json:"zone,omitempty"`
	Ratio     float64 `json:"ratio,omitempty"`
	Tab       bool    `json:"tab,omitempty"`
	Root      bool    `json:"root,omitempty"`
	Placement string  `json:"placement,omitempty"` // the run.placement this overrules
}

// runHomeFile returns the path of the per-project Run tool position record,
// following the layout store's IKE_CONFIG_DIR redirection seam.
func runHomeFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "runhome.json")
	}
	return filepath.Join(".ike", "runhome.json")
}

// loadRunHome reads the record; a missing, unreadable or malformed file is no
// error — the placement rules simply apply unchanged.
func loadRunHome() (runHome, bool) {
	data, err := os.ReadFile(runHomeFile())
	if err != nil {
		return runHome{}, false
	}
	var h runHome
	if json.Unmarshal(data, &h) != nil || (h.Anchor == "" && !h.Root) {
		return runHome{}, false
	}
	return h, true
}

// saveRunHome persists the record. Errors are swallowed like every other
// layout-state write: failing to remember a position must not disturb a run.
func saveRunHome(h runHome) {
	data, err := json.Marshal(h)
	if err != nil {
		return
	}
	path := runHomeFile()
	if dir := filepath.Dir(path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(path, data, 0o644)
}

// zoneName renders a dock zone for persistence; toolHomeZone parses it back.
func zoneName(z layout.Zone) string {
	switch z {
	case layout.ZoneLeft:
		return "left"
	case layout.ZoneRight:
		return "right"
	case layout.ZoneTop:
		return "top"
	case layout.ZoneBottom:
		return "bottom"
	}
	return ""
}

// runHomeSpot describes where the Run tool sits in the live tree, ok=false
// when none is open (or it is the sole leaf, which has no anchor to hang off).
func (m *Model) runHomeSpot() (runHome, bool) {
	locs := m.toolLocations(runToolName)
	if len(locs) == 0 {
		return runHome{}, false
	}
	loc := locs[0]
	h := runHome{Placement: m.runPlacement()}
	if loc.tab >= 0 {
		h.Anchor, h.Tab = loc.key, true
		return h, true
	}
	ws := m.activeWS()
	if ws.Tree == nil {
		return runHome{}, false
	}
	hops := layout.Hops(ws.Tree, loc.key)
	if len(hops) == 0 {
		return runHome{}, false
	}
	// The anchor is the neighbour leaf actually touching the Run pane, so
	// re-inserting beside it reproduces the position even when the sibling is
	// a whole region.
	hop := hops[0]
	h.Anchor = layout.EdgeLeafIn(hop.Sibling, layout.Opposite(hop.Zone))
	h.Zone = zoneName(layout.Opposite(hop.Zone))
	h.Ratio = hop.Ratio
	if len(hops) == 1 {
		// Hanging off the root split *is* a full-span dock (#811): remember
		// it as one, with the Run pane's own share of the workspace, so
		// reopening spans the edge again instead of splitting one neighbour.
		h.Root = true
		if hop.Zone == layout.ZoneTop || hop.Zone == layout.ZoneLeft {
			h.Ratio = 1 - hop.Ratio // the Run pane is the split's B child
		}
	}
	if h.Zone == "" || (h.Anchor == "" && !h.Root) {
		return runHome{}, false
	}
	return h, true
}

// rememberMovedRunHome persists the Run tool's position when a just-committed
// layout drag changed it. before is the spot from right before the commit:
// unchanged means the drag was about some other pane, and the record stays as
// it was (or absent, so the placement setting keeps deciding).
func (m *Model) rememberMovedRunHome(before runHome, had bool) {
	after, ok := m.runHomeSpot()
	if !ok || !had || after == before {
		return
	}
	saveRunHome(after)
}

// openRunAtRememberedHome re-opens the Run tool where the user last moved it
// (#2191). It reports false — leaving the placement rules to decide — when
// nothing was remembered, when the record belongs to a different
// `run.placement`, or when the anchor pane is gone from this workspace.
func (m *Model) openRunAtRememberedHome(sp toolSpawn) bool {
	h, ok := loadRunHome()
	if !ok || h.Placement != m.runPlacement() {
		return false
	}
	ws := m.activeWS()
	zone, ok := toolHomeZone(h.Zone)
	if ws.Tree == nil || !ok {
		return false
	}
	if !h.Root {
		if !layout.Panes(ws.Tree)[h.Anchor] || ws.Panes.Get(h.Anchor) == nil {
			return false
		}
	}
	ws.ReturnFocus = ws.Panes.Focused()
	if h.Tab {
		if !canHostTabs(ws.Panes.Get(h.Anchor)) || !m.ensureTabHost(h.Anchor) {
			return false
		}
		ws.Panes.Get(h.Anchor).AddTerminalTab(m.newToolTab(sp))
		m.finishRunHomeOpen(h.Anchor)
		return true
	}
	key := m.addToolPane(sp)
	if h.Root {
		share := h.Ratio
		if share <= 0 || share >= 1 {
			share = toolDockShare
		}
		ws.Tree = layout.DockNew(ws.Tree, key, zone, share)
		m.finishRunHomeOpen(key)
		return true
	}
	tree, ok := layout.SplitLeaf(ws.Tree, h.Anchor, key, zone)
	if !ok {
		ws.Panes.Close(key)
		return false
	}
	if h.Ratio > 0 && h.Ratio < 1 {
		// The remembered share carries over, so the Run pane comes back the
		// size the user left it — the spawnEditor convention (#1989).
		if parent := parentSplit(tree, key); parent != nil {
			parent.Ratio = h.Ratio
		}
	}
	ws.Tree = tree
	m.finishRunHomeOpen(key)
	return true
}

// finishRunHomeOpen focuses the Run tool and persists the layout — the shared
// tail of both remembered-home branches, identical to the placement openers'.
func (m *Model) finishRunHomeOpen(key string) {
	m.setFocus(key)
	m.rememberTool(runToolName, key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

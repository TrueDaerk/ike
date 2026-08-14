package app

import (
	"testing"

	"ike/internal/config"
	"ike/internal/layout"
	"ike/internal/pane"
)

// tools_home_test.go guards the configured home positions for tool windows
// (#1889): a [[tools.custom]] placement docks a fresh open against that
// workspace edge, an occupied slot adds the tool as a focused tab there, and
// moving a docked tool never rewrites the configured home.

// homeTool is a sleep-backed entry with a configured home position.
func homeTool(name, placement string) config.ToolEntry {
	e := sleepTool(name)
	e.Placement = placement
	return e
}

// closeToolInstances closes every live instance of the named tool's session.
func closeToolInstances(m Model, name string) {
	for _, loc := range m.toolLocations(name) {
		p := m.activeWS().Panes.Get(loc.key)
		if p == nil {
			continue
		}
		if loc.tab < 0 {
			p.Terminal().Close()
		} else if tt := p.TabTerminal(loc.tab); tt != nil {
			tt.Close()
		}
	}
}

// TestToolHomeFreeSlotDocks: a placement on a free edge docks the tool
// full-span against that edge instead of splitting off the editor.
func TestToolHomeFreeSlotDocks(t *testing.T) {
	for _, tc := range []struct {
		placement string
		orient    layout.Orient
		first     bool // docked leaf is child A of the root split
	}{
		{"bottom", layout.Vertical, false},
		{"top", layout.Vertical, true},
		{"right", layout.Horizontal, false},
	} {
		t.Run(tc.placement, func(t *testing.T) {
			withTools(t, homeTool("watcher", tc.placement))
			m := sized(t, 100, 40)
			out, _ := m.Update(ToolOpenMsg{Name: "watcher"})
			m = out.(Model)
			inst := m.toolPane("watcher")
			if inst == nil {
				t.Fatal("tool must open a pane")
			}
			t.Cleanup(func() { closeToolInstances(m, "watcher") })
			if m.activeWS().Panes.Focused() != inst.Key() {
				t.Fatalf("tool must take focus, focused %q", m.activeWS().Panes.Focused())
			}
			s, ok := m.activeWS().Tree.(*layout.Split)
			if !ok || s.Orient != tc.orient {
				t.Fatalf("root = %#v, want a %v dock split", m.activeWS().Tree, tc.orient)
			}
			docked := s.B
			if tc.first {
				docked = s.A
			}
			if l, ok := docked.(*layout.Leaf); !ok || l.Pane != inst.Key() {
				t.Fatalf("docked child = %#v, want the tool leaf %q", docked, inst.Key())
			}
			if got := layout.EdgeLeaf(m.activeWS().Tree, toolZone(t, tc.placement)); got != inst.Key() {
				t.Fatalf("edge occupant = %q, want %q", got, inst.Key())
			}
		})
	}
}

// toolZone resolves a placement string for test assertions.
func toolZone(t *testing.T, placement string) layout.Zone {
	t.Helper()
	z, ok := toolHomeZone(placement)
	if !ok {
		t.Fatalf("placement %q must map to a zone", placement)
	}
	return z
}

// TestToolHomeOccupiedSlotAddsTab: a second tool configured for the same edge
// joins the occupant's tab list as the focused tab — no forced split.
func TestToolHomeOccupiedSlotAddsTab(t *testing.T) {
	withTools(t, homeTool("first", "bottom"), homeTool("second", "bottom"))
	m := sized(t, 100, 40)
	out, _ := m.Update(ToolOpenMsg{Name: "first"})
	m = out.(Model)
	first := m.toolPane("first")
	if first == nil {
		t.Fatal("first tool must open a pane")
	}
	hostKey := first.Key()
	leavesBefore := len(layout.Leaves(m.activeWS().Tree))

	out, _ = m.Update(ToolOpenMsg{Name: "second"})
	m = out.(Model)
	t.Cleanup(func() {
		closeToolInstances(m, "first")
		closeToolInstances(m, "second")
	})
	if n := len(layout.Leaves(m.activeWS().Tree)); n != leavesBefore {
		t.Fatalf("leaves = %d, want %d (occupied slot must not split)", n, leavesBefore)
	}
	locs := m.toolLocations("second")
	if len(locs) != 1 || locs[0].key != hostKey || locs[0].tab < 0 {
		t.Fatalf("second tool locations = %+v, want a tab in %q", locs, hostKey)
	}
	if m.activeWS().Panes.Focused() != hostKey {
		t.Fatalf("occupant pane must take focus, focused %q", m.activeWS().Panes.Focused())
	}
	hostInst := m.activeWS().Panes.Get(hostKey)
	if tt := hostInst.TabTerminal(hostInst.ActiveTab()); tt == nil || tt.Tool() != "second" {
		t.Fatal("the new tool's tab must be active")
	}
	// The converted host still exposes the first tool for its toggle.
	if locs := m.toolLocations("first"); len(locs) != 1 || locs[0].key != hostKey {
		t.Fatalf("first tool locations = %+v, want a tab in %q", locs, hostKey)
	}
}

// TestToolHomeMoveThenReopen: dragging the docked tool elsewhere never
// rewrites the configured home — after closing it, the next open docks at the
// configured edge again.
func TestToolHomeMoveThenReopen(t *testing.T) {
	withTools(t, homeTool("watcher", "bottom"))
	m := sized(t, 100, 40)
	out, _ := m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	inst := m.toolPane("watcher")
	if inst == nil {
		t.Fatal("tool must open a pane")
	}
	// Move the tool beside the editor (the #708/0036 drag mechanics).
	m.activeWS().Tree = layout.Move(m.activeWS().Tree, inst.Key(), m.activeEditorKey(), layout.ZoneRight)
	m.layout()
	if layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneBottom) == inst.Key() {
		t.Fatal("precondition: the tool must have left the bottom dock")
	}
	if got, _ := toolEntry("watcher"); got.Placement != "bottom" {
		t.Fatalf("moving must not rewrite the configured home, placement = %q", got.Placement)
	}
	inst.Terminal().Close()
	if !m.closeKey(inst.Key()) {
		t.Fatal("closing the moved tool pane must succeed")
	}

	out, _ = m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	reopened := m.toolPane("watcher")
	if reopened == nil {
		t.Fatal("reopen must spawn a fresh pane")
	}
	t.Cleanup(func() { closeToolInstances(m, "watcher") })
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneBottom); got != reopened.Key() {
		t.Fatalf("reopen edge occupant = %q, want %q (back at the configured home)", got, reopened.Key())
	}
}

// TestToolHomeNonTabbableOccupantSharesDock: a home edge held by the explorer
// (never a tab host) stacks the tool into the same dock via a perpendicular
// split instead of tabbing or re-docking.
func TestToolHomeNonTabbableOccupantSharesDock(t *testing.T) {
	withTools(t, homeTool("watcher", "left"))
	m := sized(t, 100, 40)
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneLeft); got != pane.ExplorerKey {
		t.Fatalf("precondition: left occupant = %q, want the explorer", got)
	}
	out, _ := m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	inst := m.toolPane("watcher")
	if inst == nil {
		t.Fatal("tool must open a pane")
	}
	t.Cleanup(func() { closeToolInstances(m, "watcher") })
	s := findSplitWith(m.activeWS().Tree, inst.Key())
	if s == nil || s.Orient != layout.Vertical {
		t.Fatalf("tool split = %#v, want a vertical stack in the left dock", s)
	}
	if l, ok := s.A.(*layout.Leaf); !ok || l.Pane != pane.ExplorerKey {
		t.Fatalf("dock mate = %#v, want the explorer above", s.A)
	}
	if ex := m.activeWS().Panes.Get(pane.ExplorerKey); ex == nil || ex.Kind() != pane.KindExplorer {
		t.Fatal("the explorer pane must stay a dedicated explorer")
	}
}

// TestToolHomeUnconfiguredKeepsAuxZone: without a placement the adaptive
// heuristic (#1588) still splits the tool off the editor — no docking.
func TestToolHomeUnconfiguredKeepsAuxZone(t *testing.T) {
	withTools(t, sleepTool("watcher"))
	m := sized(t, 100, 40)
	editorKey := m.activeEditorKey()
	out, _ := m.Update(ToolOpenMsg{Name: "watcher"})
	m = out.(Model)
	inst := m.toolPane("watcher")
	if inst == nil {
		t.Fatal("tool must open a pane")
	}
	t.Cleanup(func() { closeToolInstances(m, "watcher") })
	s := findSplitWith(m.activeWS().Tree, inst.Key())
	if s == nil || s.Orient != layout.Vertical {
		t.Fatalf("tool split = %#v, want the narrow-host bottom split off the editor", s)
	}
	if l, ok := s.A.(*layout.Leaf); !ok || l.Pane != editorKey {
		t.Fatalf("split mate = %#v, want the editor", s.A)
	}
	if got := layout.EdgeLeaf(m.activeWS().Tree, layout.ZoneBottom); got != "" {
		t.Fatalf("unconfigured tool must not dock, bottom occupant = %q", got)
	}
}

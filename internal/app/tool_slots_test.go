package app

import (
	"math"
	"testing"

	"ike/internal/config"
	"ike/internal/layout"
	"ike/internal/pane"
)

// tool_slots_test.go guards named-slot tool placement (#1897): assigned
// tools pin to their template position, closed slots collapse into their
// neighbors, several tools in one slot become tabs, and unassigned tools
// keep the pre-slot behavior.

// withToolLayout installs a config carrying the tool entries plus a slot
// template with normalized "SLOT=tool" assignments (the shape validation
// produces), restoring the previous config on cleanup.
func withToolLayout(t *testing.T, template, assign []string, entries ...config.ToolEntry) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Tools.Custom = entries
	c.Tools.Layout = config.ToolLayout{Template: template, Assign: assign}
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

// issueLayout is the normative #1897 template with the explorer pinned to X.
func issueLayout(t *testing.T, assign []string, entries ...config.ToolEntry) {
	t.Helper()
	withToolLayout(t, []string{"XEEH", "XEEH", "TTZZ"}, assign, entries...)
}

func ratioNear(t *testing.T, got, want float64, what string) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("%s ratio = %v, want ~%v", what, got, want)
	}
}

// TestSlotOpenSequence walks the issue's three-step example through the real
// open path: T alone takes the full bottom strip, Z subdivides it into the
// defined TT/ZZ arrangement, closing Z hands the strip back to T, and the
// explorer snaps to its X column throughout.
func TestSlotOpenSequence(t *testing.T) {
	issueLayout(t,
		[]string{"X=explorer", "T=toolt", "Z=toolz"},
		sleepTool("toolt"), sleepTool("toolz"))
	m := sized(t, 100, 40)

	// Open T: the bottom strip spans the full width at the template's 1/3.
	m = step(m, ToolOpenMsg{Name: "toolt"})
	tKey := m.toolPane("toolt").Key()
	t.Cleanup(func() { closeToolInstances(m, "toolt") })
	root, ok := m.activeWS().Tree.(*layout.Split)
	if !ok || root.Orient != layout.Vertical {
		t.Fatalf("root = %#v, want the template's vertical strip split", m.activeWS().Tree)
	}
	ratioNear(t, root.Ratio, 2.0/3.0, "strip")
	if l, ok := root.B.(*layout.Leaf); !ok || l.Pane != tKey {
		t.Fatalf("bottom strip = %#v, want the T pane %q full-width", root.B, tKey)
	}
	top, ok := root.A.(*layout.Split)
	if !ok || top.Orient != layout.Horizontal {
		t.Fatalf("top region = %#v, want explorer|editor", root.A)
	}
	ratioNear(t, top.Ratio, 0.25, "explorer column")
	if l, ok := top.A.(*layout.Leaf); !ok || l.Pane != pane.ExplorerKey {
		t.Fatalf("X slot = %#v, want the explorer", top.A)
	}

	// Open Z: the strip subdivides into the defined TT/ZZ halves.
	m = step(m, ToolOpenMsg{Name: "toolz"})
	zKey := m.toolPane("toolz").Key()
	t.Cleanup(func() { closeToolInstances(m, "toolz") })
	root = m.activeWS().Tree.(*layout.Split)
	strip, ok := root.B.(*layout.Split)
	if !ok || strip.Orient != layout.Horizontal {
		t.Fatalf("strip = %#v, want the TT|ZZ split", root.B)
	}
	ratioNear(t, strip.Ratio, 0.5, "TT|ZZ")
	if a, ok := strip.A.(*layout.Leaf); !ok || a.Pane != tKey {
		t.Fatalf("strip left = %#v, want T %q", strip.A, tKey)
	}
	if b, ok := strip.B.(*layout.Leaf); !ok || b.Pane != zKey {
		t.Fatalf("strip right = %#v, want Z %q", strip.B, zKey)
	}

	// Close Z: the ordinary collapse gives the strip back to T.
	m.activeWS().Panes.Get(zKey).Terminal().Close()
	if !m.closeKey(zKey) {
		t.Fatal("closing Z must succeed")
	}
	root = m.activeWS().Tree.(*layout.Split)
	if l, ok := root.B.(*layout.Leaf); !ok || l.Pane != tKey {
		t.Fatalf("after close strip = %#v, want T full-width again", root.B)
	}

	// Reopen Z: the defined arrangement comes back.
	m = step(m, ToolOpenMsg{Name: "toolz"})
	zKey = m.toolPane("toolz").Key()
	root = m.activeWS().Tree.(*layout.Split)
	strip, ok = root.B.(*layout.Split)
	if !ok {
		t.Fatalf("reopen strip = %#v, want the TT|ZZ split back", root.B)
	}
	if b, ok := strip.B.(*layout.Leaf); !ok || b.Pane != zKey {
		t.Fatalf("reopen strip right = %#v, want Z %q", strip.B, zKey)
	}
}

// TestSlotSharedSlotAddsTab: the second tool assigned to an occupied slot
// joins the slot pane as the focused tab — no extra split.
func TestSlotSharedSlotAddsTab(t *testing.T) {
	issueLayout(t,
		[]string{"T=first", "T=second"},
		sleepTool("first"), sleepTool("second"))
	m := sized(t, 100, 40)
	m = step(m, ToolOpenMsg{Name: "first"})
	hostKey := m.toolPane("first").Key()
	leavesBefore := len(layout.Leaves(m.activeWS().Tree))

	m = step(m, ToolOpenMsg{Name: "second"})
	t.Cleanup(func() {
		closeToolInstances(m, "first")
		closeToolInstances(m, "second")
	})
	if n := len(layout.Leaves(m.activeWS().Tree)); n != leavesBefore {
		t.Fatalf("leaves = %d, want %d (an occupied slot must not split)", n, leavesBefore)
	}
	locs := m.toolLocations("second")
	if len(locs) != 1 || locs[0].key != hostKey || locs[0].tab < 0 {
		t.Fatalf("second tool locations = %+v, want a tab in %q", locs, hostKey)
	}
	if m.activeWS().Panes.Focused() != hostKey {
		t.Fatalf("slot pane must take focus, focused %q", m.activeWS().Panes.Focused())
	}
	hostInst := m.activeWS().Panes.Get(hostKey)
	if tt := hostInst.TabTerminal(hostInst.ActiveTab()); tt == nil || tt.Tool() != "second" {
		t.Fatal("the new tool's tab must be active")
	}
	// The converted tab host still counts as the slot's resident.
	if got := m.slotResidents()["T"]; got != hostKey {
		t.Fatalf("slot T resident = %q, want the tab host %q", got, hostKey)
	}
}

// TestSlotBuiltinPanel: a built-in tool window assigned to a slot opens at
// the slot's exact position instead of its hardcoded adaptive split.
func TestSlotBuiltinPanel(t *testing.T) {
	issueLayout(t, []string{"Z=problems"})
	m := sized(t, 100, 40)
	m = step(m, ProblemsToggleMsg{})
	root, ok := m.activeWS().Tree.(*layout.Split)
	if !ok || root.Orient != layout.Vertical {
		t.Fatalf("root = %#v, want the template's strip split", m.activeWS().Tree)
	}
	// T is closed, so the panel holds the whole strip.
	if l, ok := root.B.(*layout.Leaf); !ok || l.Pane != pane.ProblemsKey {
		t.Fatalf("strip = %#v, want the problems panel full-width", root.B)
	}
	ratioNear(t, root.Ratio, 2.0/3.0, "strip")
}

// TestSlotUnassignedToolKeepsAdaptive: a template being configured must not
// change anything for tools without an assignment.
func TestSlotUnassignedToolKeepsAdaptive(t *testing.T) {
	issueLayout(t, []string{"T=other"}, sleepTool("watcher"), sleepTool("other"))
	m := sized(t, 100, 40)
	editorKey := m.activeEditorKey()
	m = step(m, ToolOpenMsg{Name: "watcher"})
	inst := m.toolPane("watcher")
	if inst == nil {
		t.Fatal("tool must open a pane")
	}
	t.Cleanup(func() { closeToolInstances(m, "watcher") })
	s := findSplitWith(m.activeWS().Tree, inst.Key())
	if s == nil || s.Orient != layout.Vertical {
		t.Fatalf("tool split = %#v, want the adaptive bottom split off the editor", s)
	}
	if l, ok := s.A.(*layout.Leaf); !ok || l.Pane != editorKey {
		t.Fatalf("split mate = %#v, want the editor", s.A)
	}
}

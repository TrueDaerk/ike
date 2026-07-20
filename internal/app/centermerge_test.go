package app

import (
	"strings"
	"testing"
)

// centermerge_test.go covers the center drop zone (#318): during a move/tab
// drag an editor target shows five zones — the four edges split/relocate as
// before, the center merges the dragged files into the target's tab list.

// splitTabApp builds two editor panes: the source holds a.txt + b.txt, the
// target (returned second) holds c.txt, split off via a tab drag to the
// bottom edge.
func splitTabApp(t *testing.T) (Model, [3]string, string, string) {
	t.Helper()
	m, paths := tabApp(t)
	src := m.activeWS().Panes.Focused()
	r := m.lay.Panes[src]
	x, y := barCell(t, m, 17) // c.txt segment of " a.txt │ b.txt │ c.txt "
	m = step(m, press(x, y))
	m = step(m, release(r.X+r.W/2, r.Y+r.H-1))
	dst := m.activeWS().Panes.Focused()
	if dst == src {
		t.Fatal("setup: expected a split pane")
	}
	return m, paths, src, dst
}

// TestPaneDragCenterMergesTabs: a whole-pane title drag released in the
// target's center merges every source file into the target's tab list
// (deduped) and closes the emptied source pane.
func TestPaneDragCenterMergesTabs(t *testing.T) {
	m, paths, src, dst := splitTabApp(t)
	// Pre-open a.txt in the target too, so the merge has a duplicate to skip.
	m.openInTab(dst, paths[0])
	if got := m.activeWS().Panes.Get(dst).TabCount(); got != 2 {
		t.Fatalf("setup: target should hold 2 tabs, got %d", got)
	}

	sr := m.lay.Panes[src]
	dr := m.lay.Panes[dst]
	m = step(m, press(sr.X+2, sr.Y)) // grab the source pane's title bar
	m = step(m, release(dr.X+dr.W/2, dr.Y+dr.H/2))

	if _, ok := m.lay.Panes[src]; ok {
		t.Fatal("source pane should close after the center merge")
	}
	dinst := m.activeWS().Panes.Get(dst)
	if dinst == nil || dinst.TabCount() != 3 {
		t.Fatalf("target should hold c+a+b (deduped) = 3 tabs, got %v", dinst.TabCount())
	}
	for _, p := range paths {
		if dinst.TabForPath(canonicalPath(p)) < 0 {
			t.Fatalf("target is missing merged file %q", p)
		}
	}
	if m.activeWS().Panes.Focused() != dst {
		t.Fatalf("focus should land on the merge target, got %q", m.activeWS().Panes.Focused())
	}
}

// TestPaneDragEdgeStillRelocates: an editor-to-editor title drag released in
// an edge zone keeps today's relocate semantics.
func TestPaneDragEdgeStillRelocates(t *testing.T) {
	m, _, src, dst := splitTabApp(t)
	sr := m.lay.Panes[src]
	dr := m.lay.Panes[dst]
	m = step(m, press(sr.X+2, sr.Y))
	m = step(m, release(dr.X+dr.W/2, dr.Y+dr.H-1)) // bottom edge zone

	if _, ok := m.lay.Panes[src]; !ok {
		t.Fatal("edge drop must relocate, not merge away, the source pane")
	}
	if got := m.activeWS().Panes.Get(dst).TabCount(); got != 1 {
		t.Fatalf("edge drop must not merge tabs into the target, got %d", got)
	}
}

// TestTabDragToEditorEdgeSplits: a tab dropped on another editor's edge zone
// splits next to it (#318 aligns editor targets with the #317 edge rule);
// only the center merges into the tab list.
func TestTabDragToEditorEdgeSplits(t *testing.T) {
	m, paths, src, dst := splitTabApp(t)
	before := editorLeaves(m)

	m.setFocus(src)
	x, y := barCell(t, m, 1) // a.txt
	m = step(m, press(x, y))
	dr := m.lay.Panes[dst]
	m = step(m, release(dr.X+dr.W-1, dr.Y+dr.H/2)) // right edge of the target

	if got := editorLeaves(m); got != before+1 {
		t.Fatalf("edge drop should split a fresh pane: leaves %d want %d", got, before+1)
	}
	if got := m.activeWS().Panes.Get(dst).TabCount(); got != 1 {
		t.Fatalf("target's tab list must stay untouched on an edge drop, got %d", got)
	}
	if got := m.activeWS().Panes.Get(src).TabCount(); got != 1 {
		t.Fatalf("source should be down to 1 tab, got %d", got)
	}
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if ed == nil || ed.Path() != canonicalPath(paths[0]) {
		t.Fatal("the fresh split should hold the dragged file and take focus")
	}
}

// TestCenterZoneFeedback: hovering the target's center during a move drag
// shows the merge marker and a full-pane ghost, distinct from the edge zones.
func TestCenterZoneFeedback(t *testing.T) {
	m, _, src, dst := splitTabApp(t)
	sr := m.lay.Panes[src]
	dr := m.lay.Panes[dst]
	m = step(m, press(sr.X+2, sr.Y))
	m = step(m, motion(dr.X+dr.W/2, dr.Y+dr.H/2)) // hover the center

	if view := m.render(); !strings.Contains(view, "⧉ merge as tab") {
		t.Fatalf("center hover missing the merge marker:\n%s", view)
	}
	box, gx, gy, ok := m.moveGhost()
	if !ok {
		t.Fatal("expected a ghost over the center zone")
	}
	if gx != dr.X || gy != dr.Y {
		t.Fatalf("center ghost at (%d,%d), want the full pane origin (%d,%d)", gx, gy, dr.X, dr.Y)
	}
	if !strings.Contains(box, "merge as tab") {
		t.Fatalf("center ghost should carry the merge label:\n%s", box)
	}

	// An edge hover keeps the plain zone marker.
	m = step(m, motion(dr.X+dr.W/2, dr.Y+dr.H-1))
	if view := m.render(); !strings.Contains(view, "⬓ bottom") {
		t.Fatal("edge hover should show the edge-zone marker")
	}
}

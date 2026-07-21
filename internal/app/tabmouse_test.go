package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// tabmouse_test.go covers mouse support on the tab bar (#159): left-click
// focuses a tab, middle-click closes it, the wheel over the bar cycles, and
// the title row keeps working as the pane's drag handle.

// barCell returns the absolute screen cell of the focused pane's tab bar at
// bar-local offset dx.
func barCell(t *testing.T, m Model, dx int) (int, int) {
	t.Helper()
	r, ok := m.lay.Panes[m.activeWS().Panes.Focused()]
	if !ok {
		t.Fatal("focused pane has no rect")
	}
	return r.X + paneContentX + dx, r.Y + 1
}

func TestTabAtGeometry(t *testing.T) {
	labels := []string{"aa", "bb", "cc"} // segments " aa │ bb │ cc " → widths 4|1|4|1|4
	if got := tabAt(labels, 0, 40, 1); got != 0 {
		t.Fatalf("x=1 must hit tab 0, got %d", got)
	}
	if got := tabAt(labels, 0, 40, 4); got != -1 {
		t.Fatalf("x=4 is the separator, got %d", got)
	}
	if got := tabAt(labels, 0, 40, 6); got != 1 {
		t.Fatalf("x=6 must hit tab 1, got %d", got)
	}
	if got := tabAt(labels, 0, 40, 30); got != -1 {
		t.Fatalf("trailing space must miss, got %d", got)
	}
	// Overflowing bar: window around the active last tab starts with an
	// ellipsis cell, which is not a tab.
	long := []string{"first.go", "second.go", "third.go", "fourth.go", "fifth.go"}
	if got := tabAt(long, 4, 24, 0); got != -1 {
		t.Fatalf("the ellipsis cell must miss, got %d", got)
	}
}

func TestLeftClickFocusesTab(t *testing.T) {
	m, paths := tabApp(t) // three tabs, third active
	inst := m.activeWS().Panes.FocusedInstance()
	// " a.txt │ b.txt │ c.txt " — x=1 lands inside the first segment.
	x, y := barCell(t, m, 1)
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if inst.Editor().Path() != paths[0] {
		t.Fatalf("left-click on the first segment must activate tab 0, got %q", inst.Editor().Path())
	}
	if m.drag == nil || m.drag.kind != dragTab {
		t.Fatal("a tab press grabs the tab for a possible drag (#305)")
	}
	// Releasing in place is a plain click: nothing moves, no split.
	panes := len(m.lay.Panes)
	m = step(m, release(x, y))
	if m.drag != nil || len(m.lay.Panes) != panes || inst.TabCount() != 3 {
		t.Fatal("releasing on the bar must be a click, not a move")
	}
}

func TestMiddleClickClosesTab(t *testing.T) {
	m, paths := tabApp(t)
	inst := m.activeWS().Panes.FocusedInstance()
	x, y := barCell(t, m, 1)
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle})
	if inst.TabCount() != 2 {
		t.Fatalf("middle-click must close the clicked tab, tabs=%d", inst.TabCount())
	}
	if inst.EditorForPath(paths[0]) != nil {
		t.Fatal("the first tab must be the one closed")
	}
	// The closed tab fed the reopen ring (the dirty-guard path of a close).
	m = dispatch(t, m, TabReopenMsg{})
	if m.activeWS().Panes.FocusedInstance().EditorForPath(paths[0]) == nil {
		t.Fatal("a middle-click close must be reopenable")
	}
}

func TestWheelOverBarCycles(t *testing.T) {
	m, paths := tabApp(t) // third tab active
	inst := m.activeWS().Panes.FocusedInstance()
	x, y := barCell(t, m, 1)
	m = step(m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	if inst.Editor().Path() != paths[0] {
		t.Fatalf("wheel-down over the bar must cycle to the next tab, got %q", inst.Editor().Path())
	}
	m = step(m, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if inst.Editor().Path() != paths[2] {
		t.Fatalf("wheel-up must cycle back, got %q", inst.Editor().Path())
	}
	// Below the bar the wheel keeps scrolling the viewport, not the tabs.
	m = step(m, tea.MouseWheelMsg{X: x, Y: y + 3, Button: tea.MouseWheelDown})
	if inst.Editor().Path() != paths[2] {
		t.Fatal("a wheel inside the content area must not switch tabs")
	}
}

func TestActiveTabClickStartsDrag(t *testing.T) {
	m, _ := tabApp(t) // third tab active
	inst := m.activeWS().Panes.FocusedInstance()
	// Find a cell inside the active (third) segment: walk the bar until
	// tabAt reports the active index.
	r := m.lay.Panes[m.activeWS().Panes.Focused()]
	width := r.W - paneChromeW
	labels := tabLabels(inst)
	dx := -1
	for x := 0; x < width; x++ {
		if tabAt(labels, inst.ActiveTab(), width, x) == inst.ActiveTab() {
			dx = x
			break
		}
	}
	if dx < 0 {
		t.Fatal("setup: active segment not found")
	}
	x, y := barCell(t, m, dx)
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	// Since #305 a press on any tab segment grabs that tab; the whole-pane
	// drag handle is the title row and the bar outside the segments.
	if m.drag == nil || m.drag.kind != dragTab {
		t.Fatal("pressing the active tab must grab it as a tab drag (#305)")
	}
	if inst.ActiveTab() != 2 {
		t.Fatal("pressing the active tab must not switch tabs")
	}
	m = step(m, release(x, y)) // click resolves as a no-op
	rr := m.lay.Panes[m.activeWS().Panes.Focused()]
	m = step(m, press(rr.X+2, rr.Y)) // the top border row stays the pane handle
	if m.drag == nil || m.drag.kind != dragMove {
		t.Fatal("the title row must keep starting a whole-pane move")
	}
	m = step(m, release(rr.X+2, rr.Y))
}


// TestTabDragToOwnEdgeSplitsSingleFile guards #305: dragging one tab label to
// the source pane's bottom edge splits off a pane holding just that file; the
// remaining tabs stay put.
func TestTabDragToOwnEdgeSplitsSingleFile(t *testing.T) {
	m, paths := tabApp(t)
	src := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.FocusedInstance()
	r := m.lay.Panes[src]
	x, y := barCell(t, m, 1) // grab the first tab (a.txt)
	m = step(m, press(x, y))
	m = step(m, release(r.X+r.W/2, r.Y+r.H-1))

	if inst.TabCount() != 2 {
		t.Fatalf("source pane should keep 2 tabs, got %d", inst.TabCount())
	}
	newKey := m.activeWS().Panes.Focused()
	if newKey == src {
		t.Fatal("focus should land on the split pane")
	}
	if got := m.activeWS().Panes.Get(newKey).Editor().Path(); got != canonicalPath(paths[0]) {
		t.Fatalf("split pane should hold the dragged file, got %q", got)
	}
}

// terminalTabApp is tabApp plus a terminal pane split off it, refocused back on
// the editor pane; it returns the model, the tab paths, and both pane keys.
func terminalTabApp(t *testing.T) (m Model, paths [3]string, src, term string) {
	t.Helper()
	m, paths = tabApp(t)
	src = m.activeWS().Panes.Focused()
	m = dispatch(t, m, TerminalNewMsg{})
	term = m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(term)
	if inst == nil || inst.Kind() != pane.KindTerminal {
		t.Fatalf("setup: terminal.new should focus a terminal pane, got %q", term)
	}
	t.Cleanup(func() { inst.Terminal().Close() })
	m.setFocus(src)
	return m, paths, src, term
}

// TestTabDragToTerminalEdgeSplits guards #317: dropping a dragged tab on a
// terminal pane's edge zone splits the terminal there and opens the file in
// the fresh editor leaf, like a self-edge drop.
func TestTabDragToTerminalEdgeSplits(t *testing.T) {
	m, paths, src, term := terminalTabApp(t)
	inst := m.activeWS().Panes.Get(src)
	x, y := barCell(t, m, 1) // grab a.txt
	m = step(m, press(x, y))
	tr := m.lay.Panes[term]
	m = step(m, release(tr.X+tr.W/2, tr.Y+tr.H-1)) // terminal's bottom edge

	if inst.TabCount() != 2 {
		t.Fatalf("source pane should keep 2 tabs, got %d", inst.TabCount())
	}
	newKey := m.activeWS().Panes.Focused()
	if newKey == src || newKey == term {
		t.Fatalf("focus should land on the fresh split pane, got %q", newKey)
	}
	ninst := m.activeWS().Panes.Get(newKey)
	if ninst.Kind() != pane.KindEditor || ninst.Editor().Path() != canonicalPath(paths[0]) {
		t.Fatalf("split pane should be an editor holding the dragged file, got %q", ninst.Editor().Path())
	}
	if m.activeWS().Panes.Get(term).Kind() != pane.KindTerminal {
		t.Fatal("the terminal pane must survive the split")
	}
}

// TestTabDragToTerminalCenterMerges (#836, formerly the #317 interior no-op):
// a drop in a terminal's center converts it into a tab host — the shell
// session becomes the first tab and the dragged file joins beside it.
func TestTabDragToTerminalCenterMerges(t *testing.T) {
	m, paths, src, term := terminalTabApp(t)
	inst := m.activeWS().Panes.Get(src)
	tinst := m.activeWS().Panes.Get(term)
	panes := len(m.lay.Panes)
	x, y := barCell(t, m, 1)
	m = step(m, press(x, y))
	tr := m.lay.Panes[term]
	m = step(m, release(tr.X+tr.W/2, tr.Y+tr.H/2))

	if len(m.lay.Panes) != panes {
		t.Fatalf("center merge must not add panes, got %d want %d", len(m.lay.Panes), panes)
	}
	if inst.TabCount() != 2 {
		t.Fatalf("source must lose the dragged tab, got %d tabs", inst.TabCount())
	}
	if tinst.Kind() != pane.KindEditor || tinst.TabCount() != 2 {
		t.Fatalf("terminal must convert to a tab host with 2 tabs, kind=%v tabs=%d", tinst.Kind(), tinst.TabCount())
	}
	if tt := tinst.TabTerminal(0); tt == nil || !tt.Running() {
		t.Fatal("the shell session must survive the conversion as the first tab")
	}
	if ed := tinst.TabEditor(tinst.ActiveTab()); ed == nil || ed.Path() != paths[0] {
		t.Fatal("the dragged file must be the active tab of the converted pane")
	}
}

// TestTabDragOverTerminalShowsFeedback guards #317/#836: during a tab drag
// the ghost renders over a terminal target's edge zone and its interior now
// shows the center merge ghost.
func TestTabDragOverTerminalShowsFeedback(t *testing.T) {
	m, _, _, term := terminalTabApp(t)
	x, y := barCell(t, m, 1)
	m = step(m, press(x, y))
	tr := m.lay.Panes[term]
	m = step(m, motion(tr.X+tr.W/2, tr.Y+tr.H-1))
	if _, _, _, ok := m.moveGhost(); !ok {
		t.Fatal("a tab drag over a terminal edge must render the drop ghost")
	}
	m = step(m, motion(tr.X+tr.W/2, tr.Y+tr.H/2))
	box, _, _, ok := m.moveGhost()
	if !ok || !strings.Contains(box, "merge as tab") {
		t.Fatal("a terminal's interior must render the center merge ghost (#836)")
	}
	m = step(m, release(x, y))
}

// TestTabDragToOtherPaneMovesOnlyThatFile guards #305: dropping a tab on
// another editor pane relocates exactly that document.
func TestTabDragToOtherPaneMovesOnlyThatFile(t *testing.T) {
	m, paths := tabApp(t)
	src := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.FocusedInstance()
	// First create a second editor pane by tab-dragging c.txt (active) off
	// the bottom edge, then drag a.txt from the source onto it.
	r := m.lay.Panes[src]
	x, y := barCell(t, m, 17) // third segment (" a.txt │ b.txt │ c.txt ")
	m = step(m, press(x, y))
	m = step(m, release(r.X+r.W/2, r.Y+r.H-1))
	dst := m.activeWS().Panes.Focused()
	if dst == src {
		t.Fatal("setup: expected a split pane")
	}

	m.setFocus(src)
	x, y = barCell(t, m, 1) // a.txt
	m = step(m, press(x, y))
	dr := m.lay.Panes[dst]
	m = step(m, release(dr.X+dr.W/2, dr.Y+dr.H/2))

	if inst.TabCount() != 1 {
		t.Fatalf("source should be down to 1 tab, got %d", inst.TabCount())
	}
	dinst := m.activeWS().Panes.Get(dst)
	if dinst.TabCount() != 2 {
		t.Fatalf("target should have 2 tabs, got %d", dinst.TabCount())
	}
	if got := dinst.Editor().Path(); got != canonicalPath(paths[0]) {
		t.Fatalf("target's active tab should be the moved file, got %q", got)
	}
}

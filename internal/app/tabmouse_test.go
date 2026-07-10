package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// tabmouse_test.go covers mouse support on the tab bar (#159): left-click
// focuses a tab, middle-click closes it, the wheel over the bar cycles, and
// the title row keeps working as the pane's drag handle.

// barCell returns the absolute screen cell of the focused pane's tab bar at
// bar-local offset dx.
func barCell(t *testing.T, m Model, dx int) (int, int) {
	t.Helper()
	r, ok := m.lay.Panes[m.panes.Focused()]
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
	inst := m.panes.FocusedInstance()
	// " a.txt │ b.txt │ c.txt " — x=1 lands inside the first segment.
	x, y := barCell(t, m, 1)
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if inst.Editor().Path() != paths[0] {
		t.Fatalf("left-click on the first segment must activate tab 0, got %q", inst.Editor().Path())
	}
	if m.drag != nil {
		t.Fatal("a tab click must not start a pane drag")
	}
}

func TestMiddleClickClosesTab(t *testing.T) {
	m, paths := tabApp(t)
	inst := m.panes.FocusedInstance()
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
	if m.panes.FocusedInstance().EditorForPath(paths[0]) == nil {
		t.Fatal("a middle-click close must be reopenable")
	}
}

func TestWheelOverBarCycles(t *testing.T) {
	m, paths := tabApp(t) // third tab active
	inst := m.panes.FocusedInstance()
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
	inst := m.panes.FocusedInstance()
	// Find a cell inside the active (third) segment: walk the bar until
	// tabAt reports the active index.
	r := m.lay.Panes[m.panes.Focused()]
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
	if m.drag == nil || m.drag.kind != dragMove {
		t.Fatal("clicking the active tab must keep the title row as a drag handle")
	}
	if inst.ActiveTab() != 2 {
		t.Fatal("clicking the active tab must not switch tabs")
	}
}

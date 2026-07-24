package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/search"
	"ike/internal/todoindex"
)

// chrome_menus_test.go covers the chrome mouse surfaces of #1128: the tab and
// pane-title right-click context menus, the per-segment tab ✕ close button,
// and the clickable status-line segments.

// rightPress right-clicks at an absolute cell and returns the updated model.
func rightPress(m Model, x, y int) Model {
	out, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
	return out.(Model)
}

// TestTabRightClickSelectsAndCloseTargetsClickedTab guards #1128: a right
// press on a tab segment selects that tab before the menu opens, so invoking
// Close closes the clicked tab — not the one that was active before.
func TestTabRightClickSelectsAndCloseTargetsClickedTab(t *testing.T) {
	m, paths := tabApp(t) // three tabs, third active
	inst := m.activeWS().Panes.FocusedInstance()
	x, y := barCell(t, m, 1) // first segment's label
	m = rightPress(m, x, y)
	if !m.ctxMenu.IsOpen() {
		t.Fatal("right-click on a tab segment must open the context menu")
	}
	if inst.Editor().Path() != paths[0] {
		t.Fatalf("the clicked tab must be selected first, active is %q", inst.Editor().Path())
	}
	// Row 0 is "Close" (editor.closeTab); invoke it via a click inside the box.
	px, py := m.ctxMenu.Pos()
	out, cmd := m.Update(tea.MouseClickMsg{X: px + 1, Y: py + 1, Button: tea.MouseLeft})
	m = drainCmd(out.(Model), cmd)
	if inst.TabCount() != 2 {
		t.Fatalf("Close must close one tab, got %d", inst.TabCount())
	}
	if inst.EditorForPath(paths[0]) != nil {
		t.Fatal("Close must close the clicked tab, not the previously active one")
	}
	if inst.EditorForPath(paths[2]) == nil {
		t.Fatal("the previously active tab must survive")
	}
}

// TestCloseOtherTabs guards editor.tab.closeOthers (#1128): every tab but the
// active one closes.
func TestCloseOtherTabs(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("editor.tab.closeOthers"); !ok {
		t.Fatal("editor.tab.closeOthers must be a registry command")
	}
	m, paths := tabApp(t) // third active
	inst := m.activeWS().Panes.FocusedInstance()
	m = dispatch(t, m, TabCloseOthersMsg{})
	if inst.TabCount() != 1 {
		t.Fatalf("closeOthers must keep only the active tab, got %d", inst.TabCount())
	}
	if inst.Editor().Path() != paths[2] {
		t.Fatalf("the active tab must survive, got %q", inst.Editor().Path())
	}
	// The closed tabs feed the reopen ring.
	m = dispatch(t, m, TabReopenMsg{})
	if inst.TabCount() != 2 {
		t.Fatal("closeOthers victims must be reopenable")
	}
}

// TestCloseOtherTabsKeepsDirty guards #1128: a tab with unsaved changes
// survives Close Others instead of silently losing the edits.
func TestCloseOtherTabsKeepsDirty(t *testing.T) {
	m, paths := tabApp(t) // third active
	inst := m.activeWS().Panes.FocusedInstance()
	inst.TabEditor(0).RestoreText("dirty now") // a.txt dirty
	m = dispatch(t, m, TabCloseOthersMsg{})
	if inst.TabCount() != 2 {
		t.Fatalf("the dirty tab must survive closeOthers, got %d tabs", inst.TabCount())
	}
	if inst.EditorForPath(paths[0]) == nil {
		t.Fatal("the dirty tab must stay open")
	}
	if inst.EditorForPath(paths[1]) != nil {
		t.Fatal("the clean other tab must close")
	}
}

// TestPaneTitleRightClickOpensPaneMenu guards #1128: a right press on the
// title band outside the tab segments opens the pane menu, and Close Pane
// closes the whole pane.
func TestPaneTitleRightClickOpensPaneMenu(t *testing.T) {
	m, _ := tabApp(t)
	key := m.activeWS().Panes.Focused()
	r := m.lay.Panes[key]
	// The bar " a.txt ✕ │ b.txt ✕ │ c.txt ✕ " is 29 cells; press well past it.
	m = rightPress(m, r.X+r.W-paneContentX-1, r.Y+1)
	if !m.ctxMenu.IsOpen() {
		t.Fatal("right-click on the title band must open the pane menu")
	}
	// Row 3 is "Close Pane" (pane.close).
	px, py := m.ctxMenu.Pos()
	out, cmd := m.Update(tea.MouseClickMsg{X: px + 1, Y: py + 1 + 3, Button: tea.MouseLeft})
	m = drainCmd(out.(Model), cmd)
	if _, ok := m.lay.Panes[key]; ok {
		t.Fatal("Close Pane must close the clicked pane whole")
	}
}

// TestPaneMenuSplitRightSplits guards #1128: the pane menu's Split Right
// entry dispatches pane.splitRight for the pane whose band was clicked.
func TestPaneMenuSplitRightSplits(t *testing.T) {
	m, _ := tabApp(t)
	key := m.activeWS().Panes.Focused()
	r := m.lay.Panes[key]
	panes := len(m.lay.Panes)
	m = rightPress(m, r.X+r.W-paneContentX-1, r.Y+1)
	px, py := m.ctxMenu.Pos()
	out, cmd := m.Update(tea.MouseClickMsg{X: px + 1, Y: py + 1, Button: tea.MouseLeft})
	m = drainCmd(out.(Model), cmd)
	if len(m.lay.Panes) != panes+1 {
		t.Fatalf("Split Right must add a pane, got %d want %d", len(m.lay.Panes), panes+1)
	}
}

// TestClosePaneGuardsDirty guards #1128: pane.close on a pane holding unsaved
// changes opens the close guard with a whole-pane pending close; discarding
// then closes the pane with all its tabs.
func TestClosePaneGuardsDirty(t *testing.T) {
	m, _ := tabApp(t)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.FocusedInstance()
	inst.TabEditor(0).RestoreText("dirty now")
	m = dispatch(t, m, ClosePaneMsg{})
	if m.closePending == nil || m.closePending.tab != -1 {
		t.Fatal("pane.close on a dirty pane must queue a whole-pane guarded close")
	}
	if _, ok := m.lay.Panes[key]; !ok {
		t.Fatal("the pane must stay open while the guard prompts")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if _, ok := m.lay.Panes[key]; ok {
		t.Fatal("discarding must close the pane whole, not just the active tab")
	}
}

// closeZoneCell finds the bar-local x of tab idx's ✕ zone via the same
// hit-test the mouse path uses.
func closeZoneCell(t *testing.T, m Model, idx int) int {
	t.Helper()
	inst := m.activeWS().Panes.FocusedInstance()
	r := m.lay.Panes[m.activeWS().Panes.Focused()]
	labels := tabLabels(inst)
	for x := 0; x < r.W-paneChromeW; x++ {
		if i, on := tabHit(labels, inst.ActiveTab(), r.W-paneChromeW, x); i == idx && on {
			return x
		}
	}
	t.Fatalf("no ✕ zone found for tab %d", idx)
	return -1
}

// TestTabCloseButtonClickClosesTab guards #1128: a left press on a segment's
// ✕ zone closes that tab without changing the active one; the label cells
// keep focusing (TestLeftClickFocusesTab).
func TestTabCloseButtonClickClosesTab(t *testing.T) {
	m, paths := tabApp(t) // third active
	inst := m.activeWS().Panes.FocusedInstance()
	// First segment " a.txt ✕ ": pad 0, label 1-5, pad 6, ✕ 7.
	if dx := closeZoneCell(t, m, 0); dx != 7 {
		t.Fatalf("a.txt's ✕ must sit at bar cell 7, got %d", dx)
	}
	x, y := barCell(t, m, closeZoneCell(t, m, 0))
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if inst.TabCount() != 2 {
		t.Fatalf("the ✕ zone must close the tab, got %d tabs", inst.TabCount())
	}
	if inst.EditorForPath(paths[0]) != nil {
		t.Fatal("the clicked segment's tab must be the one closed")
	}
	if inst.Editor().Path() != paths[2] {
		t.Fatalf("the active tab must not change, got %q", inst.Editor().Path())
	}
}

// TestTabCloseButtonGuardsDirty guards #1128: the ✕ zone of a dirty tab opens
// the unsaved-changes guard on that tab instead of dropping the edits.
func TestTabCloseButtonGuardsDirty(t *testing.T) {
	m, _ := tabApp(t)
	inst := m.activeWS().Panes.FocusedInstance()
	inst.TabEditor(0).RestoreText("dirty now")
	x, y := barCell(t, m, closeZoneCell(t, m, 0)) // "a.txt ●"'s ✕
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if inst.TabCount() != 3 {
		t.Fatal("a dirty tab's ✕ must not close it outright")
	}
	if m.closePending == nil {
		t.Fatal("a dirty tab's ✕ must open the close guard")
	}
	if inst.ActiveTab() != 0 {
		t.Fatal("the guard must target the clicked tab (selected first)")
	}
}

// TestComposeStatusSpans guards #1128: the span list mirrors the rendered
// line — each span's cells hold exactly its segment's text.
func TestComposeStatusSpans(t *testing.T) {
	left := []renderedSeg{{id: "mode", text: "NORMAL"}, {id: "todo", text: "3 TODOs"}}
	right := []renderedSeg{{id: "cursor", text: "Ln 1, Col 1"}}
	line, spans := composeStatusSpans(left, right, 80)
	if len(spans) != 3 {
		t.Fatalf("want 3 spans, got %d", len(spans))
	}
	texts := map[string]string{"mode": "NORMAL", "todo": "3 TODOs", "cursor": "Ln 1, Col 1"}
	r := []rune(line)
	for _, s := range spans {
		if got := string(r[s.x0:s.x1]); got != texts[s.id] {
			t.Fatalf("span %s covers %q, want %q", s.id, got, texts[s.id])
		}
	}
	if spans[2].x1 != 79 {
		t.Fatalf("the right group must end one pad before the edge, x1=%d", spans[2].x1)
	}
}

// statusTodoApp opens a file, seeds the TODO index with one finished scan
// result and closes the overlay, so the status line shows a TODO segment.
func statusTodoApp(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	m := openApp(t, writeTemp(t, dir, "a.txt", "aaa\n"))
	m = dispatch(t, m, OpenTodoIndexMsg{})
	m = dispatch(t, m, todoindex.ScanMsg{Inner: search.BatchMsg{Gen: 1, Matches: []search.Match{
		{Path: "x.go", Line: 1, Text: "// TODO: x", StartCol: 3, EndCol: 7},
	}}})
	m = dispatch(t, m, todoindex.ScanMsg{Inner: search.DoneMsg{Gen: 1, Total: 1}})
	m.todo.Close()
	return m
}

// TestStatusSegmentClickDispatches guards #1128: a left press on the TODO
// segment of the status row dispatches todo.list; a press between segments
// dispatches nothing and never leaks into the panes.
func TestStatusSegmentClickDispatches(t *testing.T) {
	m := statusTodoApp(t)
	x := -1
	for i := 0; i < m.width; i++ {
		if m.statusSegmentAt(i) == "todo" {
			x = i
			break
		}
	}
	if x < 0 {
		t.Fatal("setup: no todo segment on the status row")
	}
	out, cmd := m.Update(tea.MouseClickMsg{X: x, Y: m.height - 1, Button: tea.MouseLeft})
	m = out.(Model)
	found := false
	for _, msg := range cmdMsgs(cmd) {
		if _, ok := msg.(OpenTodoIndexMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("clicking the TODO segment must dispatch todo.list")
	}
	// The gap in the middle of the row belongs to no segment: swallowed.
	before := m.activeWS().Panes.Focused()
	out, cmd = m.Update(tea.MouseClickMsg{X: m.width / 2, Y: m.height - 1, Button: tea.MouseLeft})
	m = out.(Model)
	if cmd != nil || m.activeWS().Panes.Focused() != before {
		t.Fatal("a press between segments must be swallowed")
	}
}

// TestStatusNotificationsClickDispatches guards #1128: the unseen counter
// segment dispatches notifications.history.
func TestStatusNotificationsClickDispatches(t *testing.T) {
	dir := t.TempDir()
	m := openApp(t, writeTemp(t, dir, "a.txt", "aaa\n"))
	m.notifUnseen = 2
	x := -1
	for i := 0; i < m.width; i++ {
		if m.statusSegmentAt(i) == "notifications" {
			x = i
			break
		}
	}
	if x < 0 {
		t.Fatal("setup: no notifications segment on the status row")
	}
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: m.height - 1, Button: tea.MouseLeft})
	found := false
	for _, msg := range cmdMsgs(cmd) {
		if _, ok := msg.(ShowNotificationHistoryMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("clicking the notifications counter must dispatch notifications.history")
	}
}

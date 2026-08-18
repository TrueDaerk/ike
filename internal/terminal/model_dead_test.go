package terminal

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// model_dead_test.go — the exited pane as a read-only view of the finished
// run (#1951): selection/copy, scrollback paging and resize reflow while the
// session is dead, plus the dialog click mapping following the recomputed
// geometry.

// deadRun builds a finished run session at w×h that printed script's output
// first. The model is one column wider: the rightmost column is the pane's
// scrollbar gutter, so the grid is exactly w wide.
func deadRun(t *testing.T, w, h int, script string, tool bool) *Model {
	t.Helper()
	c := &collector{}
	s, err := StartCommandSession("terminal", []string{"/bin/sh", "-c", script}, t.TempDir(), w, h, nil, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	m := &Model{w: w + 1, h: h, sess: s, send: c.send}
	if tool {
		m.SetTool("claude")
	}
	waitFor(t, "session exit", func() bool { return !s.Running() })
	waitFor(t, "run output on the grid", func() bool {
		return strings.Contains(ansi.Strip(s.View()), "line-") ||
			strings.Contains(ansi.Strip(s.HistoryLine(0)), "line-")
	})
	return m
}

// printLines is a script printing n numbered lines.
func printLines(n int) string {
	return "i=1; while [ $i -le " + strconv.Itoa(n) + " ]; do echo line-$i; i=$((i+1)); done"
}

// TestDeadPaneCopiesSelection: the exited pane is selectable and yields its
// selection as text — what the app's copy chord puts on the clipboard.
func TestDeadPaneCopiesSelection(t *testing.T) {
	m := deadRun(t, 40, 10, printLines(3), true)
	m.MousePress(0, 0)
	m.MouseDrag(6, 0)
	m.MouseRelease(6, 0)
	if !m.HasSelection() {
		t.Fatal("a dead pane must anchor a mouse selection")
	}
	if got := m.SelectionText(); got != "line-1" {
		t.Fatalf("selection text = %q, want %q", got, "line-1")
	}
}

// TestDeadPaneWheelScrollsScrollback: the wheel pages the finished session's
// own history instead of going to the child that is gone.
func TestDeadPaneWheelScrollsScrollback(t *testing.T) {
	m := deadRun(t, 40, 6, printLines(40), true)
	if m.ScrollbackLen() == 0 {
		t.Fatal("40 lines in a 6-row pane must produce scrollback")
	}
	m.MouseWheel(0, 0, 5)
	if m.Scroll() != 5 {
		t.Fatalf("wheel scroll = %d, want 5", m.Scroll())
	}
	m.MouseWheel(0, 0, -5)
	if m.Scroll() != 0 {
		t.Fatalf("wheel back to live = %d, want 0", m.Scroll())
	}
}

// TestDeadPaneKeysScrollScrollback: the paging keys work while dead, and an
// inert key no longer snaps the view back to live.
func TestDeadPaneKeysScrollScrollback(t *testing.T) {
	m := deadRun(t, 40, 6, printLines(40), true)
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if want := m.pageSize(); m.Scroll() != want {
		t.Fatalf("pgup scroll = %d, want %d", m.Scroll(), want)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if want := m.pageSize() + 1; m.Scroll() != want {
		t.Fatalf("up scroll = %d, want %d", m.Scroll(), want)
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if want := m.pageSize() + 1; m.Scroll() != want {
		t.Fatalf("an inert key must keep the scroll position, got %d want %d", m.Scroll(), want)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if m.Scroll() != m.ScrollbackLen() {
		t.Fatalf("home scroll = %d, want %d", m.Scroll(), m.ScrollbackLen())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.Scroll() != 0 {
		t.Fatalf("end scroll = %d, want 0", m.Scroll())
	}
}

// TestDeadPaneScrolledKeepsDialog: paging the output keeps the exit dialog —
// and therefore its click targets — in place.
func TestDeadPaneScrolledKeepsDialog(t *testing.T) {
	m := deadRun(t, 60, 12, printLines(40), true)
	m.MouseWheel(0, 0, 4)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "claude exited") {
		t.Fatalf("scrolled dead view must keep the dialog, got:\n%s", view)
	}
	g, ok := m.deadDialogGeom()
	if !ok {
		t.Fatal("dialog geometry must survive scrolling")
	}
	if got := m.DeadActionHit(g.restartX+1, g.btnRow); got != "restart" {
		t.Fatalf("restart hit while scrolled = %q", got)
	}
}

// TestDeadPaneResizeReflowsAndRecenters: dragging the divider while dead
// reflows the content and re-centers the dialog; the click mapping follows
// the recomputed geometry, the stale one no longer hits.
func TestDeadPaneResizeReflowsAndRecenters(t *testing.T) {
	m := deadRun(t, 60, 12, printLines(20), true)
	before, ok := m.deadDialogGeom()
	if !ok {
		t.Fatal("60x12 must fit the dialog")
	}
	m.SetSize(100, 30)
	waitFor(t, "reflow at the new width", func() bool { return m.sess.Width() == 99 })
	view := ansi.Strip(m.View())
	if rows := len(strings.Split(view, "\n")); rows != 30 {
		t.Fatalf("dead view renders %d rows, want 30", rows)
	}
	if !strings.Contains(view, "line-20") {
		t.Fatalf("the reflow must keep the run's output, got:\n%s", view)
	}
	after, ok := m.deadDialogGeom()
	if !ok {
		t.Fatal("dialog must still fit the grown pane")
	}
	if after.x == before.x || after.y == before.y {
		t.Fatalf("dialog must re-center: before (%d,%d), after (%d,%d)",
			before.x, before.y, after.x, after.y)
	}
	if got := m.DeadActionHit(after.restartX+1, after.btnRow); got != "restart" {
		t.Fatalf("restart hit after resize = %q", got)
	}
	if got := m.DeadActionHit(before.restartX+1, before.btnRow); got == "restart" {
		t.Fatal("stale pre-resize geometry must not hit the button")
	}
}

// TestDeadRunPaneFooterResizes: a plain run pane (no tool) reflows to the new
// size while dead and keeps its exit footer.
func TestDeadRunPaneFooterResizes(t *testing.T) {
	m := deadRun(t, 40, 8, printLines(3), false)
	m.SetSize(70, 14)
	waitFor(t, "reflow at the new width", func() bool { return m.sess.Width() == 69 })
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "line-3") {
		t.Fatalf("the reflow must keep the run's output, got:\n%s", view)
	}
	if rows := len(strings.Split(view, "\n")); rows != 14 {
		t.Fatalf("dead run view renders %d rows, want 14", rows)
	}
	if !strings.Contains(view, "process exited with code 0") {
		t.Fatalf("dead run view must keep its exit footer, got:\n%s", view)
	}
}

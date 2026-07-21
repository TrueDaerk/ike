package terminal

import (
	"strings"
	"testing"
)

// deadTool builds a finished tool-marked command session at w×h.
func deadTool(t *testing.T, w, h int) *Model {
	t.Helper()
	c := &collector{}
	s, err := StartCommandSession("terminal", []string{"/bin/sh", "-c", "exit 3"}, t.TempDir(), w, h, nil, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	m := &Model{w: w, h: h, sess: s, send: c.send}
	m.SetTool("claude")
	waitFor(t, "session exit", func() bool { return !s.Running() })
	return m
}

// TestDeadToolDialogRendersCentered guards the #810 follow-up: the exit
// dialog is a centered box (prominent in large panes), names the tool and
// exit code, and its buttons hit-test where they render.
func TestDeadToolDialogRendersCentered(t *testing.T) {
	m := deadTool(t, 120, 40)
	view := m.View()
	if !strings.Contains(view, "claude exited (code 3)") {
		t.Fatalf("dialog must name tool and code, view: %q", view)
	}
	if !strings.Contains(view, deadRestartBtn) || !strings.Contains(view, deadCloseBtn) {
		t.Fatal("dialog must render both buttons")
	}
	g, ok := m.deadDialogGeom()
	if !ok {
		t.Fatal("large pane must use the dialog")
	}
	if g.y == 0 || g.x == 0 {
		t.Fatalf("dialog must be centered, got origin (%d,%d)", g.x, g.y)
	}
	if got := m.DeadActionHit(g.restartX+1, g.btnRow); got != "restart" {
		t.Fatalf("restart hit = %q", got)
	}
	if got := m.DeadActionHit(g.closeX+1, g.btnRow); got != "close" {
		t.Fatalf("close hit = %q", got)
	}
	if got := m.DeadActionHit(g.restartX+1, g.btnRow-1); got != "" {
		t.Fatalf("row above buttons must not hit, got %q", got)
	}
}

// TestDeadToolSmallPaneFallsBackToFooter: a pane too small for the dialog
// keeps the footer line and its hit spans.
func TestDeadToolSmallPaneFallsBackToFooter(t *testing.T) {
	m := deadTool(t, 30, 4)
	if _, ok := m.deadDialogGeom(); ok {
		t.Fatal("30x4 must be too small for the dialog")
	}
	if !strings.Contains(m.View(), "[restart (r)]") {
		t.Fatalf("small pane must fall back to the footer, view: %q", m.View())
	}
	found := false
	for y := 0; y <= 4 && !found; y++ {
		for x := 0; x < 30; x++ {
			if m.DeadActionHit(x, y) == "restart" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("footer fallback must keep a restart hit zone")
	}
}

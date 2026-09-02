package app

import (
	"strings"
	"testing"

	"ike/internal/project"
)

// project_switchlast_test.go covers project.switchLast (#2398): the alt+tab of
// project switching — resume the MRU background workspace, and bounce back on
// the next invocation.

// TestSwitchLastPicksMRUBackground guards the pick and the toggle: after a
// switch a -> b, switchLast returns to a, and invoking it again lands on b.
// Both hops are full switches, so the workspaces resume instead of being
// rebuilt.
func TestSwitchLastPicksMRUBackground(t *testing.T) {
	a, b := twoProjects(t)
	m := switchModel(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	if !sameDir(t, cwd(t), b) {
		t.Fatalf("setup: cwd = %s, want b", cwd(t))
	}
	wsA := m.ws.Peek(m.ws.Background()[len(m.ws.Background())-1])
	if wsA == nil {
		t.Fatal("setup: a must be parked in the background")
	}

	// Back to a — the MRU parked root.
	out, _ = m.Update(project.SwitchLastMsg{})
	m = out.(Model)
	if !sameDir(t, cwd(t), a) {
		t.Fatalf("switchLast landed in %s, want a", cwd(t))
	}
	if m.activeWS() != wsA {
		t.Fatal("switchLast must resume the parked workspace, not rebuild it")
	}

	// And again — back to b: the toggle.
	out, _ = m.Update(project.SwitchLastMsg{})
	m = out.(Model)
	if !sameDir(t, cwd(t), b) {
		t.Fatalf("second switchLast landed in %s, want b", cwd(t))
	}
}

// TestSwitchLastRecordsHistory guards that the hop is a normal switch, not a
// peek (#2136): the resumed project lands at the head of project.history.
func TestSwitchLastRecordsHistory(t *testing.T) {
	a, b := twoProjects(t)
	m := switchModel(t)

	out, _ := m.Update(project.SwitchProjectMsg{Root: b})
	m = out.(Model)
	out, cmd := m.Update(project.SwitchLastMsg{})
	m = out.(Model)
	if !sameDir(t, cwd(t), a) {
		t.Fatalf("switchLast landed in %s, want a", cwd(t))
	}
	if cmd == nil {
		t.Fatal("a recorded switch must return the history-write command")
	}
	if m.peek != nil {
		t.Fatal("switchLast must not mark the workspace as a peek")
	}
}

// TestSwitchLastWithoutBackgroundNotifies guards the empty case: nothing
// changes and the user is told why.
func TestSwitchLastWithoutBackgroundNotifies(t *testing.T) {
	a, _ := twoProjects(t)
	m := switchModel(t)
	if len(m.ws.Background()) != 0 {
		t.Fatal("setup: a single project has no background workspace")
	}
	active := m.activeWS()

	out, _ := m.Update(project.SwitchLastMsg{})
	m = out.(Model)
	if !sameDir(t, cwd(t), a) {
		t.Fatalf("cwd = %s, want the untouched a", cwd(t))
	}
	if m.activeWS() != active {
		t.Fatal("the active workspace must be untouched")
	}
	if len(m.history) == 0 || !strings.Contains(m.history[len(m.history)-1].text, "no previous project") {
		t.Fatalf("expected a 'no previous project' notification, history = %+v", m.history)
	}
}

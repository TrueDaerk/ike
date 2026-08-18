package app

// terminal_slots_test.go covers the #1946 assignable targets: an assigned
// "terminal" slot is where fresh integrated-terminal panes open — the first
// materializes the slot pane, further ones join it as tabs — and run/debug
// place independently when assigned to different slots.

import (
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// TestSlotTerminalOpensAtSlot: with T=terminal, terminal.new materializes
// the fresh shell pane at the T slot (the full bottom strip while its
// neighbor slots are closed), and a second terminal.new joins it as a
// focused tab instead of splitting.
func TestSlotTerminalOpensAtSlot(t *testing.T) {
	issueLayout(t, []string{"T=terminal"})
	m := sized(t, 100, 40)

	m = step(m, TerminalNewMsg{})
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindTerminal || inst.Terminal().Tool() != "" {
		t.Fatalf("terminal.new must focus a plain shell pane, got %q", key)
	}
	t.Cleanup(func() {
		if inst := m.activeWS().Panes.Get(key); inst != nil {
			inst.CloseTerminalTabs()
		}
	})
	root, ok := m.activeWS().Tree.(*layout.Split)
	if !ok || root.Orient != layout.Vertical {
		t.Fatalf("root = %#v, want the template's vertical strip split", m.activeWS().Tree)
	}
	ratioNear(t, root.Ratio, 2.0/3.0, "strip")
	if l, ok := root.B.(*layout.Leaf); !ok || l.Pane != key {
		t.Fatalf("bottom strip = %#v, want the terminal pane %q full-width", root.B, key)
	}
	if got := m.slotResidents()["T"]; got != key {
		t.Fatalf("T resident = %q, want the terminal pane %q", got, key)
	}

	// The second terminal joins the slot pane as a focused tab.
	leavesBefore := len(layout.Leaves(m.activeWS().Tree))
	m = step(m, TerminalNewMsg{})
	if n := len(layout.Leaves(m.activeWS().Tree)); n != leavesBefore {
		t.Fatalf("leaves = %d, want %d (tab join, no extra split)", n, leavesBefore)
	}
	host := m.activeWS().Panes.Get(key)
	if host == nil || host.TabCount() != 2 || host.ActiveTerminal() == nil {
		t.Fatalf("slot pane must host 2 terminal tabs with the new one active, got %+v", host)
	}
	if got := m.slotResidents()["T"]; got != key {
		t.Fatalf("T resident after tab join = %q, want %q", got, key)
	}
}

// TestSlotTerminalUnassignedKeepsAdaptive: without a terminal assignment the
// pre-#1946 adaptive split stays untouched.
func TestSlotTerminalUnassignedKeepsAdaptive(t *testing.T) {
	issueLayout(t, []string{"X=explorer"})
	m := sized(t, 100, 40)
	m = step(m, TerminalNewMsg{})
	key := m.activeWS().Panes.Focused()
	t.Cleanup(func() {
		if inst := m.activeWS().Panes.Get(key); inst != nil {
			inst.CloseTerminalTabs()
		}
	})
	if got := m.slotResidents()["T"]; got != "" {
		t.Fatalf("an unassigned terminal must not claim a slot, T resident = %q", got)
	}
}

// TestSlotRunAndDebugSeparate: run and debug are independently assignable —
// the Run tool lands in its slot, the debug panel in another.
func TestSlotRunAndDebugSeparate(t *testing.T) {
	issueLayout(t, []string{"Z=run", "T=debug"})
	m := runModel(t, "bottom")
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	runInst := m.activeWS().Panes.FocusedInstance()
	if runInst == nil || runInst.Kind() != pane.KindTerminal || runInst.Terminal().Tool() != runToolName {
		t.Fatal("RunFileMsg must open the Run tool pane")
	}

	m.openDebugPanel()
	if !m.activeWS().Panes.Has(pane.DebugKey) {
		t.Fatal("openDebugPanel must create the debug panel pane")
	}

	residents := m.slotResidents()
	if got := residents["Z"]; got != runInst.Key() {
		t.Fatalf("Z resident = %q, want the Run tool pane %q", got, runInst.Key())
	}
	if got := residents["T"]; got != pane.DebugKey {
		t.Fatalf("T resident = %q, want the debug panel %q", got, pane.DebugKey)
	}
}

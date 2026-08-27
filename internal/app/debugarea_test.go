package app

import (
	"testing"

	"ike/internal/dap"
	"ike/internal/debugpanel"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
)

// debugarea_test.go covers the combined debug area (#2190): composition (one
// pane hosting panel + console), reuse across sessions, and the configurable
// session-end behavior.

// TestDebugAreaComposition: a stop opens exactly one new leaf — the combined
// area — whose panel embeds the console; the console view exposes the
// terminal to the pane seams (ActiveTerminal) while the variables view keeps
// the panel's own routing.
func TestDebugAreaComposition(t *testing.T) {
	m, _, path := debugModel(t)
	before := len(layout.Leaves(m.activeWS().Tree))
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2, Column: 1}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	if got := len(layout.Leaves(m.activeWS().Tree)); got != before+1 {
		t.Fatalf("leaves = %d, want %d — one combined area, not a pane pair", got, before+1)
	}
	p := m.debugPanel()
	if p == nil || !p.HasTerm() {
		t.Fatal("the area must open with its console installed")
	}
	inst := m.activeWS().Panes.Get(pane.DebugKey)
	if inst.ActiveTerminal() != nil {
		t.Fatal("the variables view must not expose the console as active terminal")
	}
	p.SetTab(debugpanel.TabConsole)
	if inst.ActiveTerminal() == nil {
		t.Fatal("the console view must expose the embedded terminal")
	}
}

// TestDebugAreaScrollbackSurvivesViewSwitch: output fed to the console stays
// in its scrollback across Variables↔Console switches — the terminal model
// moves nowhere (#2190).
func TestDebugAreaScrollbackSurvivesViewSwitch(t *testing.T) {
	m, _, path := debugModel(t)
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2, Column: 1}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	tm, _ = m.Update(debugEventMsg{ev: dap.Event{
		Name: "output",
		Body: []byte(`{"category":"stdout","output":"kept across switches\n"}`),
	}})
	m = tm.(Model)
	p := m.debugPanel()
	term := p.Term()
	waitViewContains(t, term, "kept across switches")
	p.SetTab(debugpanel.TabConsole)
	p.SetTab(debugpanel.TabVariables)
	p.SetTab(debugpanel.TabConsole)
	if p.Term() != term {
		t.Fatal("view switches must keep the same terminal model")
	}
	waitViewContains(t, term, "kept across switches")
}

// TestDebugAreaReusedAcrossSessions: a second session's stop reuses the
// still-open area instead of opening another pane.
func TestDebugAreaReusedAcrossSessions(t *testing.T) {
	m, _, path := debugModel(t)
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2, Column: 1}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	tm, _ = m.Update(debugEndedMsg{exitCode: 0, hasCode: true})
	m = tm.(Model)
	if p := m.debugPanel(); p == nil || !p.Finished() {
		t.Fatal("precondition: the area survives session end finished")
	}
	after := len(layout.Leaves(m.activeWS().Tree))
	// A new session (the test harness installs it directly, like debugModel).
	pipe, _ := startStub(t)
	sess := dap.NewSession(dap.NewConn(pipe, nil))
	if err := sess.Initialize(); err != nil {
		t.Fatal(err)
	}
	m.dbg = &debugState{sess: sess, cfgName: "prog.rfake", root: projectRoot()}
	tm, _ = m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	if got := len(layout.Leaves(m.activeWS().Tree)); got != after {
		t.Fatalf("leaves = %d after the second session's stop, want %d (area reused)", got, after)
	}
	if p := m.debugPanel(); p == nil || p.Finished() {
		t.Fatal("the reused area must leave the finished state on the new stop")
	}
}

// TestDebugSessionEndCloseTidiesLayout: debug.session_end = "close" removes
// the area when the session ends; the default keeps it (covered above).
func TestDebugSessionEndCloseTidiesLayout(t *testing.T) {
	m, _, path := debugModelWith(t, host.MapConfig{"debug.session_end": "close"})
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2, Column: 1}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	if !m.activeWS().Panes.Has(pane.DebugKey) {
		t.Fatal("precondition: the stop opens the area")
	}
	tm, _ = m.Update(debugEndedMsg{exitCode: 0, hasCode: true})
	m = tm.(Model)
	if m.activeWS().Panes.Has(pane.DebugKey) {
		t.Fatal("session_end=close must remove the debug area on termination")
	}
	for _, leaf := range layout.Leaves(m.activeWS().Tree) {
		if leaf == pane.DebugKey {
			t.Fatal("the closed area must leave the tree")
		}
	}
}

// TestDebugSessionEndCloseOnStop: the explicit debug.stop honours the close
// setting too.
func TestDebugSessionEndCloseOnStop(t *testing.T) {
	m, _, path := debugModelWith(t, host.MapConfig{"debug.session_end": "close"})
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2, Column: 1}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	tm, _ = m.Update(DebugStopMsg{})
	m = tm.(Model)
	if m.activeWS().Panes.Has(pane.DebugKey) {
		t.Fatal("session_end=close must remove the debug area on debug.stop")
	}
}

// TestDebugConsoleCommandTogglesView: the debug.console command flips the
// area's view and focuses it — the keyboard route that works even while a PTY
// debuggee eats raw keys (#2190).
func TestDebugConsoleCommandTogglesView(t *testing.T) {
	m, _, path := debugModel(t)
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2, Column: 1}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	if p := m.debugPanel(); p.ConsoleActive() {
		t.Fatal("precondition: a stop lands on the variables view")
	}
	tm, _ = m.Update(DebugConsoleMsg{})
	m = tm.(Model)
	if p := m.debugPanel(); !p.ConsoleActive() {
		t.Fatal("debug.console must switch to the console view")
	}
	if m.activeWS().Panes.Focused() != pane.DebugKey {
		t.Fatal("debug.console must focus the area")
	}
	tm, _ = m.Update(DebugConsoleMsg{})
	m = tm.(Model)
	if p := m.debugPanel(); p.ConsoleActive() {
		t.Fatal("a second debug.console must switch back to variables")
	}
}

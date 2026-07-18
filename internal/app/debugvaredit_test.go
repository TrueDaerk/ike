package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
	"ike/internal/debugpanel"
	"ike/internal/pane"
)

// stopAt drives one stopped message through the model and returns it paused
// with the debug panel open.
func stopAt(t *testing.T, m Model, path string) Model {
	t.Helper()
	frames := []dap.StackFrame{{ID: 1, Name: "f", Source: dap.Source{Path: path}, Line: 2, Column: 1}}
	tm, _ := m.Update(debugStoppedMsg{threadID: 1, frames: frames})
	m = tm.(Model)
	if m.debugPanel() == nil {
		t.Fatal("a stop must open the debug panel")
	}
	return m
}

// TestSetVarMsgSendsSetVariableRequest verifies the app-level wiring (#640):
// a committed edit reaches the adapter as a setVariable request, followed by
// the refresh fetch of the containing reference.
func TestSetVarMsgSendsSetVariableRequest(t *testing.T) {
	m, sa, path := debugModel(t)
	m = stopAt(t, m, path)
	tm, _ := m.Update(debugpanel.SetVarMsg{Ref: 100, Name: "x", Value: "99"})
	m = tm.(Model)
	waitForCommand(t, sa, "setVariable")
	waitForCommand(t, sa, "variables") // the post-set refresh
}

// TestSetVarRefusedWhenNotPaused verifies the paused gate (#640): an edit
// commit while the debuggee runs never reaches the adapter and surfaces an
// Info notice instead.
func TestSetVarRefusedWhenNotPaused(t *testing.T) {
	m, sa, _ := debugModel(t)
	if m.dbg.paused {
		t.Fatal("precondition: session must not be paused")
	}
	tm, _ := m.Update(debugpanel.SetVarMsg{Ref: 100, Name: "x", Value: "99"})
	m = tm.(Model)
	time.Sleep(50 * time.Millisecond)
	for _, c := range sa.commands() {
		if c == "setVariable" {
			t.Fatal("setVariable while running must not reach the adapter")
		}
	}
	// The refusal surfaces as a notice; an Update pass drains it into a toast.
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	found := false
	for _, tst := range m.toasts {
		if strings.Contains(tst.text, "not paused") {
			found = true
		}
	}
	if !found {
		t.Fatalf("refusal must surface a notice, toasts: %+v", m.toasts)
	}
}

// TestRestoredPanelBecomesEditable verifies #640 defect 1: a debug panel that
// pre-exists (restored from a saved layout before any session) still receives
// the SetEditable gate when a session's first stop attaches to it.
func TestRestoredPanelBecomesEditable(t *testing.T) {
	m, _, path := debugModel(t)
	// Simulate the restored layout: the panel exists before the session runs.
	dbg := m.dbg
	m.dbg = nil
	m.openDebugPanel()
	p := m.debugPanel()
	if p == nil {
		t.Fatal("panel must open without a session (restore path)")
	}
	if p.Editable() {
		t.Fatal("precondition: no session, no editable gate yet")
	}
	m.dbg = dbg
	m = stopAt(t, m, path)
	if !m.debugPanel().Editable() {
		t.Fatal("the first stop must gate editing on the pre-existing panel")
	}
}

// TestContinuedEventGatesPanel verifies #640 defect 3b under the #693
// semantics: a spontaneous continued event keeps the last stop's rows visible
// as stale context, but nothing stale is activatable or editable while the
// debuggee runs.
func TestContinuedEventGatesPanel(t *testing.T) {
	m, _, path := debugModel(t)
	m = stopAt(t, m, path)
	if _, ok := m.debugPanel().SelectedFrame(); !ok {
		t.Fatal("precondition: the stop must feed frames")
	}
	tm, _ := m.Update(debugEventMsg{ev: dap.Event{Name: "continued"}})
	m = tm.(Model)
	if m.dbg.paused {
		t.Fatal("continued must clear the paused state")
	}
	p := m.debugPanel()
	if _, ok := p.SelectedFrame(); !ok {
		t.Fatal("continued must keep the stale frames visible (#693)")
	}
	if p.Editing() {
		t.Fatal("continued must close an inline editor")
	}
	if !strings.Contains(p.View(), "running") {
		t.Fatal("continued must show the running indicator over the stale rows")
	}
}

// TestDebugEditEscDoesNotArmPalette verifies #640 defect 6: the esc that
// cancels an inline variable edit is consumed by the panel and must not count
// toward the double-esc palette shortcut.
func TestDebugEditEscDoesNotArmPalette(t *testing.T) {
	m, _, path := debugModel(t)
	m.dbg.paused = true
	m = stopAt(t, m, path)
	tm, _ := m.Update(debugScopesMsg{scopes: []dap.Scope{{Name: "Locals", VariablesReference: 100}}})
	m = tm.(Model)
	tm, _ = m.Update(debugVarsMsg{ref: 100, vars: []dap.Variable{{Name: "x", Value: "42"}}})
	m = tm.(Model)
	m.setFocus(pane.DebugKey)
	p := m.debugPanel()
	// Drive the panel into an edit: variables column, child row, 'e'.
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	p.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	if !p.Editing() {
		t.Fatal("precondition: the inline editor must be open")
	}
	// First esc cancels the edit; a quick second esc must not open the palette.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.debugPanel().Editing() {
		t.Fatal("esc must cancel the inline edit")
	}
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.palette.IsOpen() {
		t.Fatal("the edit-cancelling esc must not arm the double-esc palette")
	}
}

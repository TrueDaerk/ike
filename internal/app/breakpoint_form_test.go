package app

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/breakpanel"
	"ike/internal/dap"
	"ike/internal/debug"
)

// allRefinementCaps is what a supporting adapter (delve, debugpy) advertises.
var allRefinementCaps = map[string]any{
	"supportsConditionalBreakpoints":    true,
	"supportsHitConditionalBreakpoints": true,
	"supportsLogPoints":                 true,
}

// formModel opens a breakpoint on the model's file and hands back the model
// with the properties form open on it (#2245).
func formModel(t *testing.T, caps map[string]any) (Model, *stubAdapter, string) {
	t.Helper()
	debugCaps(t, caps)
	m, sa, path := debugModel(t)
	m.bpts.Toggle(bpKey(path), 1)
	tm, _ := m.Update(breakpanel.EditMetaMsg{Path: bpKey(path), Line: 1})
	m = tm.(Model)
	if !m.breakpointFormOpen() {
		t.Fatal("the properties form did not open")
	}
	return m, sa, path
}

// TestBreakpointFormEditsPersistsAndSyncs walks the three fields, applies
// them, and checks the store, the disk and the live session all see it.
func TestBreakpointFormEditsPersistsAndSyncs(t *testing.T) {
	m, sa, path := formModel(t, allRefinementCaps)
	m = typeInto(m, "i > 3")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "%2")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeInto(m, "i is {i}")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.breakpointFormOpen() {
		t.Fatal("apply must close the form")
	}
	got := m.bpts.MetaAt(bpKey(path), 1)
	want := debug.Meta{Condition: "i > 3", HitCondition: "%2", LogMessage: "i is {i}"}
	if got != want {
		t.Fatalf("stored meta = %+v, want %+v", got, want)
	}
	// Persisted: a reload of the store sees the same refinements.
	if reloaded := debug.Load().MetaAt(bpKey(path), 1); reloaded != want {
		t.Fatalf("reloaded meta = %+v, want %+v", reloaded, want)
	}
	// Pushed to the live session with all three fields intact.
	waitForCommand(t, sa, "setBreakpoints")
	reqs := sa.breakpointRequests()
	var args struct {
		Breakpoints []dap.SourceBreakpoint `json:"breakpoints"`
	}
	if err := json.Unmarshal(reqs[len(reqs)-1], &args); err != nil {
		t.Fatal(err)
	}
	if len(args.Breakpoints) != 1 {
		t.Fatalf("adapter saw %+v, want one breakpoint", args.Breakpoints)
	}
	bp := args.Breakpoints[0]
	if bp.Line != 2 || bp.Condition != "i > 3" || bp.HitCondition != "%2" || bp.LogMessage != "i is {i}" {
		t.Fatalf("adapter saw %+v", bp)
	}
	if !containsSubstr(notices(m), "logpoint set") {
		t.Fatalf("notices = %v, want the logpoint confirmation", notices(m))
	}
}

// TestBreakpointFormRejectsInvalidHitCount keeps the form open with the
// reason and the focus on the offending field (#2245).
func TestBreakpointFormRejectsInvalidHitCount(t *testing.T) {
	m, _, path := formModel(t, allRefinementCaps)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab}) // onto the hit count
	m = typeInto(m, "lots")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if !m.breakpointFormOpen() {
		t.Fatal("an invalid hit count must keep the form open")
	}
	if m.bpForm.err == "" {
		t.Fatal("no reason recorded")
	}
	if m.bpForm.field != bpFieldHit {
		t.Fatalf("focus = %v, want the offending field", m.bpForm.field)
	}
	if !m.bpts.MetaAt(bpKey(path), 1).IsZero() {
		t.Fatal("a rejected form must not touch the store")
	}
}

// TestBreakpointFormRejectsBlankCondition is the empty-but-enabled case: a
// condition of spaces would reach the adapter and fail the breakpoint.
func TestBreakpointFormRejectsBlankCondition(t *testing.T) {
	m, _, _ := formModel(t, allRefinementCaps)
	m = typeInto(m, "   ")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.breakpointFormOpen() || m.bpForm.field != bpFieldCondition {
		t.Fatal("a blank condition must be rejected on its own field")
	}
}

// TestBreakpointFormGatesUnsupportedFields disables the fields the adapter
// did not advertise, says so, skips them on tab — and still saves the ones it
// supports, with the session intact (#2245).
func TestBreakpointFormGatesUnsupportedFields(t *testing.T) {
	// The stub advertises only conditional breakpoints.
	m, sa, path := formModel(t, map[string]any{"supportsConditionalBreakpoints": true})

	if !containsSubstr(notices(m), "does not support hit counts, logpoints") {
		t.Fatalf("notices = %v, want the disabled-field notice", notices(m))
	}
	if m.bpForm.field != bpFieldCondition {
		t.Fatalf("focus = %v, want the one supported field", m.bpForm.field)
	}
	view := m.shell.View()
	if !strings.Contains(view, "unsupported by adapter") {
		t.Fatalf("the form does not mark the disabled fields:\n%s", view)
	}
	// Tab cannot reach a disabled field.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.bpForm.field != bpFieldCondition {
		t.Fatalf("tab reached the disabled field %v", m.bpForm.field)
	}
	m = typeInto(m, "i > 3")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.bpts.MetaAt(bpKey(path), 1); got.Condition != "i > 3" || got.LogMessage != "" {
		t.Fatalf("stored meta = %+v, want just the condition", got)
	}
	// The session still gets the breakpoint — with the condition, without the
	// fields it cannot honour.
	waitForCommand(t, sa, "setBreakpoints")
	reqs := sa.breakpointRequests()
	var args struct {
		Breakpoints []dap.SourceBreakpoint `json:"breakpoints"`
	}
	if err := json.Unmarshal(reqs[len(reqs)-1], &args); err != nil {
		t.Fatal(err)
	}
	if len(args.Breakpoints) != 1 || args.Breakpoints[0].Condition != "i > 3" {
		t.Fatalf("adapter saw %+v", args.Breakpoints)
	}
}

// TestBreakpointFormAllFieldsDisabled keeps a capability-less adapter's form
// read-only and harmless: the stored values stay visible and untouched.
func TestBreakpointFormAllFieldsDisabled(t *testing.T) {
	debugCaps(t, nil)
	m, _, path := debugModel(t)
	m.bpts.Toggle(bpKey(path), 1)
	m.bpts.SetMeta(bpKey(path), 1, debug.Meta{Condition: "i > 3"})
	tm, _ := m.Update(breakpanel.EditMetaMsg{Path: bpKey(path), Line: 1})
	m = tm.(Model)

	if m.bpForm.field != bpFieldCount {
		t.Fatalf("focus = %v, want no editable field", m.bpForm.field)
	}
	m = typeInto(m, "xyz")
	if m.bpForm.vals[bpFieldCondition] != "i > 3" {
		t.Fatalf("a read-only form accepted input: %q", m.bpForm.vals[bpFieldCondition])
	}
	if !strings.Contains(m.shell.View(), "i > 3") {
		t.Fatalf("the stored value is not shown:\n%s", m.shell.View())
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.breakpointFormOpen() {
		t.Fatal("esc must close the form")
	}
	if m.bpts.MetaAt(bpKey(path), 1).Condition != "i > 3" {
		t.Fatal("the store must survive an unsupported-capability form untouched")
	}
}

// TestBreakpointPropertiesWithoutBreakpoint refuses the chord on a bare line.
func TestBreakpointPropertiesWithoutBreakpoint(t *testing.T) {
	debugCaps(t, allRefinementCaps)
	m, _, _ := debugModel(t)
	clearNotices(&m)
	tm, _ := m.Update(DebugBreakpointPropertiesMsg{})
	m = tm.(Model)
	if m.breakpointFormOpen() {
		t.Fatal("a line without a breakpoint must not open the form")
	}
	if !containsSubstr(notices(m), "no breakpoint on") {
		t.Fatalf("notices = %v", notices(m))
	}
}

// TestBreakpointPropertiesAtCursorOpensForm is the gutter chord on a line
// that does carry a breakpoint (#2245).
func TestBreakpointPropertiesAtCursorOpensForm(t *testing.T) {
	debugCaps(t, allRefinementCaps)
	m, _, path := debugModel(t)
	m.bpts.Toggle(bpKey(path), 0) // the cursor sits on line 0 after opening
	tm, _ := m.Update(DebugBreakpointPropertiesMsg{})
	m = tm.(Model)
	if !m.breakpointFormOpen() || m.bpForm.line != 0 {
		t.Fatalf("form state = %+v", m.bpForm)
	}
}

// TestStrippedRefinementNoticeOnSessionStart warns once when the store holds
// refinements the adapter cannot honour, and still sends the breakpoints.
func TestStrippedRefinementNoticeOnSessionStart(t *testing.T) {
	debugCaps(t, nil) // an adapter advertising none of the three
	m, sa, path := debugModel(t)
	m.bpts.Toggle(bpKey(path), 1)
	m.bpts.SetMeta(bpKey(path), 1, debug.Meta{Condition: "i > 3", LogMessage: "hi"})
	clearNotices(&m)

	tm, _ := m.Update(debugEventMsg{sess: m.dbg.sess, ev: dap.Event{Name: "initialized"}})
	m = tm.(Model)

	if !containsSubstr(notices(m), "does not support conditions, log messages") {
		t.Fatalf("notices = %v, want the stripped-refinement warning", notices(m))
	}
	waitForCommand(t, sa, "setBreakpoints")
	reqs := sa.breakpointRequests()
	var args struct {
		Breakpoints []dap.SourceBreakpoint `json:"breakpoints"`
	}
	if err := json.Unmarshal(reqs[len(reqs)-1], &args); err != nil {
		t.Fatal(err)
	}
	if len(args.Breakpoints) != 1 || args.Breakpoints[0].Line != 2 {
		t.Fatalf("adapter saw %+v, want the plain breakpoint", args.Breakpoints)
	}
	if args.Breakpoints[0].Condition != "" || args.Breakpoints[0].LogMessage != "" {
		t.Fatalf("unsupported refinements reached the adapter: %+v", args.Breakpoints[0])
	}
}

// TestNoStrippedNoticeWhenSupported keeps the session-start quiet for an
// adapter that honours everything.
func TestNoStrippedNoticeWhenSupported(t *testing.T) {
	debugCaps(t, allRefinementCaps)
	m, sa, path := debugModel(t)
	m.bpts.Toggle(bpKey(path), 1)
	m.bpts.SetMeta(bpKey(path), 1, debug.Meta{Condition: "i > 3"})
	clearNotices(&m)

	tm, _ := m.Update(debugEventMsg{sess: m.dbg.sess, ev: dap.Event{Name: "initialized"}})
	m = tm.(Model)
	if containsSubstr(notices(m), "does not support") {
		t.Fatalf("notices = %v, want no capability warning", notices(m))
	}
	waitForCommand(t, sa, "setBreakpoints")
}

// TestLogpointOutputDoesNotPause is the logpoint contract end to end: the
// adapter logs through an output event and never stops, so the session keeps
// running and the transcript carries the message (#2245).
func TestLogpointOutputDoesNotPause(t *testing.T) {
	debugCaps(t, allRefinementCaps)
	m, _, path := debugModel(t)
	m.bpts.Toggle(bpKey(path), 1)
	m.bpts.SetMeta(bpKey(path), 1, debug.Meta{LogMessage: "i is {i}"})
	tm, _ := m.Update(debugEventMsg{sess: m.dbg.sess, ev: dap.Event{Name: "initialized"}})
	m = tm.(Model)

	body, err := json.Marshal(dap.OutputEvent{Category: "stdout", Output: "i is 7\n"})
	if err != nil {
		t.Fatal(err)
	}
	tm, _ = m.Update(debugEventMsg{sess: m.dbg.sess, ev: dap.Event{Name: "output", Body: body}})
	m = tm.(Model)

	if m.dbg == nil || m.dbg.paused {
		t.Fatal("a logpoint's output must not pause the session")
	}
	if _, ok := m.activeEditor().PausedLine(); ok {
		t.Fatal("a logpoint must not set the paused marker")
	}
}

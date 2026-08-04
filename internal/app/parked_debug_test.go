package app

import (
	"encoding/json"
	"testing"

	"ike/internal/dap"
	"ike/internal/host"
	"ike/internal/registry"
	"ike/internal/workspace"
)

func outputEvent(category, text string) dap.Event {
	body, _ := json.Marshal(map[string]string{"category": category, "output": text})
	return dap.Event{Name: "output", Body: body}
}

// TestParkedDebugEventRouting guards #1523: adapter events carry their owning
// session, and one from a parked workspace's debuggee never touches the
// active workspace's session state — output lands in the parked workspace's
// capped buffer, state events are consumed without effect.
func TestParkedDebugEventRouting(t *testing.T) {
	root := t.TempDir()
	m := NewWith(registry.New(), host.MapConfig{})

	parkedSess := &dap.Session{}
	parkedDbg := &debugState{sess: parkedSess, root: root}
	active := m.ws.Active()
	m.ws.SetActive(workspace.New(root, active.Panes))
	m.ws.Active().Aux = wsExtras{dbg: parkedDbg}
	m.ws.Park()
	m.ws.SetActive(active)

	activeSess := &dap.Session{}
	m.dbg = &debugState{sess: activeSess, root: "/tmp/active"}

	// Parked output: buffered in the parked workspace, active untouched.
	m.handleDebugEvent(parkedSess, outputEvent("stdout", "parked says hi\n"))
	if len(parkedDbg.pendingOut) != 1 || parkedDbg.pendingOut[0].text != "parked says hi\n" {
		t.Fatalf("parked output not buffered: %+v", parkedDbg.pendingOut)
	}
	if len(m.dbg.pendingOut) != 0 {
		t.Fatalf("active session received the parked output: %+v", m.dbg.pendingOut)
	}

	// Parked state event: consumed, active pause state untouched.
	m.handleDebugEvent(parkedSess, dap.Event{Name: "continued"})
	if m.dbg.paused {
		t.Fatal("parked 'continued' must not touch the active session")
	}

	// Stop/ended follow-ups of a foreign session are dropped.
	if m.applyDebugStop(debugStoppedMsg{sess: parkedSess, threadID: 7}) != nil || m.dbg.paused {
		t.Fatal("foreign debugStoppedMsg must be dropped")
	}
	m.finishDebugSession(debugEndedMsg{sess: parkedSess})
	if m.dbg == nil {
		t.Fatal("foreign debugEndedMsg must not finish the active session")
	}

	// The active session's own events still apply.
	m.handleDebugEvent(activeSess, outputEvent("telemetry", "noise"))
	if len(m.dbg.pendingOut) != 0 {
		t.Fatal("telemetry must stay filtered")
	}
	m.finishDebugSession(debugEndedMsg{sess: activeSess})
	if m.dbg != nil {
		t.Fatal("the active session's ended message must finish it")
	}
}

// TestParkedOutputCapped guards the bound (#1523): a firehose from a parked
// debuggee stays at maxPendingOut chunks, oldest dropped.
func TestParkedOutputCapped(t *testing.T) {
	root := t.TempDir()
	m := NewWith(registry.New(), host.MapConfig{})
	parkedSess := &dap.Session{}
	parkedDbg := &debugState{sess: parkedSess}
	active := m.ws.Active()
	m.ws.SetActive(workspace.New(root, active.Panes))
	m.ws.Active().Aux = wsExtras{dbg: parkedDbg}
	m.ws.Park()
	m.ws.SetActive(active)

	for i := 0; i < maxPendingOut+50; i++ {
		m.handleDebugEvent(parkedSess, outputEvent("stdout", "x"))
	}
	if len(parkedDbg.pendingOut) != maxPendingOut {
		t.Fatalf("parked buffer = %d chunks, want cap %d", len(parkedDbg.pendingOut), maxPendingOut)
	}
}

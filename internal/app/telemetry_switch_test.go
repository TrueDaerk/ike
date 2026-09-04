package app

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/telemetry"
)

// telemetry_switch_test.go covers the project-switch op lifecycle (#2403):
// the switch, the close and the incoming project's LSP warm-up.

// opsOf filters op events by operation id.
func opsOf(evs []telemetry.Event, id string) []telemetry.Event {
	var out []telemetry.Event
	for _, ev := range eventsOf(evs, telemetry.TypeOp) {
		if ev.Data["id"] == id {
			out = append(out, ev)
		}
	}
	return out
}

// TestTelemetryProjectSwitchOp is the #2403 criterion: a project switch
// brackets itself with a start/ok op pair carrying a non-negative ms and the
// structural detail fields the export needs to explain a slow switch.
func TestTelemetryProjectSwitchOp(t *testing.T) {
	t.Chdir(t.TempDir()) // performSwitch chdirs into the target; restore after
	m := telemetryModel(t, host.MapConfig{})
	m.usage.Command("before.switch", telemetry.SourceInternal) // open the session file

	tm, _ := m.performSwitch(t.TempDir())
	fresh := tm.(Model)

	ops := opsOf(usageEvents(t, fresh), telemetry.OpProjectSwitch)
	if len(ops) != 3 {
		t.Fatalf("want start, end and lsp phase, got %v", ops)
	}
	if ops[0].Data["phase"] != "start" {
		t.Fatalf("first phase = %v, want start", ops[0])
	}
	if _, ok := ops[0].Data["ms"]; ok {
		t.Errorf("start op carries a duration: %v", ops[0])
	}
	end := ops[1]
	if end.Data["phase"] != "ok" {
		t.Fatalf("end phase = %v, want ok", end)
	}
	ms, err := strconv.ParseInt(end.Data["ms"], 10, 64)
	if err != nil || ms < 0 {
		t.Errorf("end ms = %q (%v), want a non-negative number", end.Data["ms"], err)
	}
	if end.Data["parked"] != "false" {
		t.Errorf("first visit reported as parked: %v", end)
	}
	if panes, err := strconv.Atoi(end.Data["panes"]); err != nil || panes < 0 {
		t.Errorf("panes = %q (%v), want a count", end.Data["panes"], err)
	}
	if end.Data["lsp"] != "-1" {
		t.Errorf("lsp = %q, want -1 before any publish", end.Data["lsp"])
	}
	// The warm-up phase is total since #2492: a model with no server-language
	// document open reports it skipped on the spot instead of arming a wait
	// that could never resolve.
	if ops[2].Data["phase"] != "lsp" || ops[2].Data["skipped"] != "no_server_docs" {
		t.Errorf("third op = %v, want an lsp phase with skipped=no_server_docs", ops[2])
	}
	if fresh.switchLSPWait != nil {
		t.Error("a docless switch must not leave the warm-up wait armed")
	}
	// The privacy line (#2235) holds here too: structural tokens only.
	for _, ev := range ops {
		for _, v := range ev.Data {
			if strings.Contains(v, string(os.PathSeparator)) {
				t.Errorf("switch op carries a path: %v", ev)
			}
		}
	}
}

// TestTelemetryProjectSwitchOpReportsParked: the switch back into a parked
// workspace says so — the flag is what separates a resume from a cold first
// visit in the export.
func TestTelemetryProjectSwitchOpReportsParked(t *testing.T) {
	first := t.TempDir()
	t.Chdir(first)
	m := telemetryModel(t, host.MapConfig{})
	m.usage.Command("before.switch", telemetry.SourceInternal)

	tm, _ := m.performSwitch(t.TempDir()) // parks `first`
	back, _ := tm.(Model).performSwitch(first)

	ops := opsOf(usageEvents(t, back.(Model)), telemetry.OpProjectSwitch)
	if len(ops) != 6 {
		t.Fatalf("want two start/end/lsp triples, got %v", ops)
	}
	if got := ops[4].Data["parked"]; got != "true" {
		t.Errorf("resume reported parked=%q, want true", got)
	}
}

// lspPhasesOf filters a model's recorded switch ops down to the "lsp" phase.
func lspPhasesOf(t *testing.T, m Model) []telemetry.Event {
	t.Helper()
	var lsp []telemetry.Event
	for _, ev := range opsOf(usageEvents(t, m), telemetry.OpProjectSwitch) {
		if ev.Data["phase"] == "lsp" {
			lsp = append(lsp, ev)
		}
	}
	return lsp
}

// TestTelemetryProjectSwitchLSPPhase: the warm-up of the project switched into
// lands as its own phase on the same op id, so "the switch was instant but the
// editor stayed diagnostic-blind" is visible in the export.
func TestTelemetryProjectSwitchLSPPhase(t *testing.T) {
	t.Chdir(t.TempDir())
	m := telemetryModel(t, host.MapConfig{})
	m.usage.Command("before.switch", telemetry.SourceInternal)
	// Armed like a switch into a project with server-language documents does —
	// the switch itself is exercised in TestTelemetryProjectSwitchOp; this
	// covers the publish side of the wait.
	m.switchLSPWait = &switchLSPWait{start: time.Now()}

	out, _ := m.Update(ilsp.DiagnosticsMsg{Path: "/somewhere/a.go"})
	settled := out.(Model)
	if settled.switchLSPWait != nil {
		t.Error("the warm-up timer must disarm on the first publish")
	}
	out, _ = settled.Update(ilsp.DiagnosticsMsg{Path: "/somewhere/b.go"}) // must not report twice

	lsp := lspPhasesOf(t, out.(Model))
	if len(lsp) != 1 {
		t.Fatalf("want exactly one lsp phase, got %v", lsp)
	}
	if ms, err := strconv.ParseInt(lsp[0].Data["ms"], 10, 64); err != nil || ms < 0 {
		t.Errorf("lsp ms = %q (%v), want a non-negative number", lsp[0].Data["ms"], err)
	}
	if _, ok := lsp[0].Data["skipped"]; ok {
		t.Errorf("a real publish measurement must not carry skipped: %v", lsp[0])
	}
}

// TestTelemetryProjectSwitchLSPQuietFallback (#2492): a wait no publish ever
// closes is resolved by the quiet timer — but only the timer belonging to the
// armed wait; a stale one from an earlier switch is ignored.
func TestTelemetryProjectSwitchLSPQuietFallback(t *testing.T) {
	t.Chdir(t.TempDir())
	m := telemetryModel(t, host.MapConfig{})
	m.usage.Command("before.switch", telemetry.SourceInternal)
	m.switchLSPWait = &switchLSPWait{start: time.Now()}

	out, _ := m.Update(switchLSPQuietMsg{wait: &switchLSPWait{}}) // stale timer
	stale := out.(Model)
	if stale.switchLSPWait == nil {
		t.Fatal("a stale quiet timer must not disarm the current wait")
	}
	out, _ = stale.Update(switchLSPQuietMsg{wait: stale.switchLSPWait})
	settled := out.(Model)
	if settled.switchLSPWait != nil {
		t.Error("the matching quiet timer must disarm the wait")
	}

	lsp := lspPhasesOf(t, settled)
	if len(lsp) != 1 || lsp[0].Data["skipped"] != "quiet" {
		t.Fatalf("want exactly one lsp phase with skipped=quiet, got %v", lsp)
	}
}

// TestTelemetryProjectSwitchLSPSuperseded (#2492): a switch that starts while
// the previous switch's warm-up is still unresolved closes that wait as
// superseded, so the earlier op stays phase-complete in the export.
func TestTelemetryProjectSwitchLSPSuperseded(t *testing.T) {
	t.Chdir(t.TempDir())
	m := telemetryModel(t, host.MapConfig{})
	m.usage.Command("before.switch", telemetry.SourceInternal)
	m.switchLSPWait = &switchLSPWait{start: time.Now()}

	tm, _ := m.performSwitch(t.TempDir())

	lsp := lspPhasesOf(t, tm.(Model))
	if len(lsp) != 2 {
		t.Fatalf("want the superseded phase plus the new switch's own, got %v", lsp)
	}
	if lsp[0].Data["skipped"] != "superseded" {
		t.Errorf("first lsp phase = %v, want skipped=superseded", lsp[0])
	}
}

// TestTelemetryProjectCloseOp: project.close is timed too, and its span wraps
// the switch it runs through, so the teardown can be priced separately.
func TestTelemetryProjectCloseOp(t *testing.T) {
	first := t.TempDir()
	t.Chdir(first)
	m := telemetryModel(t, host.MapConfig{})
	m.usage.Command("before.switch", telemetry.SourceInternal)

	tm, _ := m.performSwitch(t.TempDir()) // parks `first`, so a close has a target
	next, _ := tm.(Model).handleCloseProject()

	ops := opsOf(usageEvents(t, next.(Model)), telemetry.OpProjectClose)
	if len(ops) != 2 || ops[0].Data["phase"] != "start" || ops[1].Data["phase"] != "ok" {
		t.Fatalf("want a start/ok close pair, got %v", ops)
	}
	if ms, err := strconv.ParseInt(ops[1].Data["ms"], 10, 64); err != nil || ms < 0 {
		t.Errorf("close ms = %q (%v), want a non-negative number", ops[1].Data["ms"], err)
	}
}

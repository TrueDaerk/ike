package app

import (
	"os"
	"strconv"
	"strings"
	"testing"

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
	if len(ops) != 2 {
		t.Fatalf("want a start and an end op, got %v", ops)
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
	if len(ops) != 4 {
		t.Fatalf("want two start/end pairs, got %v", ops)
	}
	if got := ops[3].Data["parked"]; got != "true" {
		t.Errorf("resume reported parked=%q, want true", got)
	}
}

// TestTelemetryProjectSwitchLSPPhase: the warm-up of the project switched into
// lands as its own phase on the same op id, so "the switch was instant but the
// editor stayed diagnostic-blind" is visible in the export.
func TestTelemetryProjectSwitchLSPPhase(t *testing.T) {
	t.Chdir(t.TempDir())
	m := telemetryModel(t, host.MapConfig{})
	m.usage.Command("before.switch", telemetry.SourceInternal)

	tm, _ := m.performSwitch(t.TempDir())
	fresh := tm.(Model)
	if fresh.switchLSPWait == nil {
		t.Fatal("switch armed no LSP warm-up timer")
	}
	out, _ := fresh.Update(ilsp.DiagnosticsMsg{Path: "/somewhere/a.go"})
	settled := out.(Model)
	if settled.switchLSPWait != nil {
		t.Error("the warm-up timer must disarm on the first publish")
	}
	out, _ = settled.Update(ilsp.DiagnosticsMsg{Path: "/somewhere/b.go"}) // must not report twice

	var lsp []telemetry.Event
	for _, ev := range opsOf(usageEvents(t, out.(Model)), telemetry.OpProjectSwitch) {
		if ev.Data["phase"] == "lsp" {
			lsp = append(lsp, ev)
		}
	}
	if len(lsp) != 1 {
		t.Fatalf("want exactly one lsp phase, got %v", lsp)
	}
	if ms, err := strconv.ParseInt(lsp[0].Data["ms"], 10, 64); err != nil || ms < 0 {
		t.Errorf("lsp ms = %q (%v), want a non-negative number", lsp[0].Data["ms"], err)
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

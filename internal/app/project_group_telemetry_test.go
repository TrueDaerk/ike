package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/config"
	"ike/internal/telemetry"
)

// project_group_telemetry_test.go covers the group-level ops (0510, #2578):
// project.group.open with its members / skipped / landed_on tally and
// project.group.close with its member count, plus the rule that a disabled
// recorder writes nothing at all.

// groupOps returns this model's op events for id, in file order. The package
// shares one temp HOME, so the telemetry directory accumulates every test's
// events — the session id is what keeps one test's chain apart from another's.
func groupOps(t *testing.T, m Model, id string) []telemetry.Event {
	t.Helper()
	m.usage.Flush()
	sid := m.usage.SessionID()
	dir := telemetryDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []telemetry.Event
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var ev telemetry.Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("bad JSONL line %q: %v", line, err)
			}
			if ev.SID == sid && ev.Type == telemetry.TypeOp && ev.Data["id"] == id {
				out = append(out, ev)
			}
		}
	}
	return out
}

// opPhases lists the phases of the events in order, for the start/end shape.
func opPhases(evs []telemetry.Event) []string {
	var out []string
	for _, ev := range evs {
		out = append(out, ev.Data["phase"])
	}
	return out
}

// TestGroupOpenRecordsOp: a three-member open records one start and one ok
// carrying the member count, no skips, the landing member's project token and
// the chain's total ms — while every hop still records its own project.switch.
func TestGroupOpenRecordsOp(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui", "www")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)

	m, _ = openGroup(t, m, "web")

	evs := groupOps(t, m, telemetry.OpProjectGroupOpen)
	if got := opPhases(evs); len(got) != 2 || got[0] != "start" || got[1] != "ok" {
		t.Fatalf("phases = %v, want [start ok]", got)
	}
	d := evs[1].Data
	if d["members"] != "3" || d["skipped"] != "0" {
		t.Errorf("payload = %v, want members 3 and skipped 0", d)
	}
	if len(d["landed_on"]) != 12 {
		t.Errorf("landed_on = %q, want a 12-hex project token", d["landed_on"])
	}
	if d["landed_on"] != telemetry.ProjectToken(cwd(t)) {
		t.Errorf("landed_on = %q, want the token of the landing root %s", d["landed_on"], cwd(t))
	}
	if _, ok := d["ms"]; !ok {
		t.Errorf("the end phase must carry ms, got %v", d)
	}
	// The chain is the sum of its hops: each one keeps its own op.
	if n := len(groupOps(t, m, telemetry.OpProjectSwitch)); n < 2 {
		t.Errorf("every hop records a project.switch op, got %d events", n)
	}
}

// TestGroupOpenOpCountsSkippedMember: a member that is gone is one skipped hop
// in the payload, and the members count stays the present ones the chain ran.
func TestGroupOpenOpCountsSkippedMember(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can enter any directory")
	}
	// A directory that validates (readable) but cannot be entered fails its
	// hop's chdir: present at resolve time, skipped at hop time.
	roots := peekFixture(t, "origin", "api", "locked", "www")
	if err := os.Chmod(roots[2], 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roots[2], 0o755) })
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)

	m, _ = openGroup(t, m, "web")

	evs := groupOps(t, m, telemetry.OpProjectGroupOpen)
	if len(evs) != 2 {
		t.Fatalf("want a start and an end, got %v", opPhases(evs))
	}
	d := evs[1].Data
	if d["phase"] != "ok" || d["members"] != "3" || d["skipped"] != "1" {
		t.Fatalf("payload = %v, want ok with members 3 and skipped 1", d)
	}
}

// TestGroupCloseRecordsOp: the close op carries the number of member
// workspaces that were torn down, the switched-away one included.
func TestGroupCloseRecordsOp(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui")
	storeGroup(t, "web", roots[1:])
	m := switchModel(t)
	m, _ = openGroup(t, m, "web")

	out, cmd := m.handleCloseGroup()
	m, _ = driveGroupOpen(t, out.(Model), cmd)

	evs := groupOps(t, m, telemetry.OpProjectGroupClose)
	if len(evs) < 2 {
		t.Fatalf("want a start and an end, got %v", opPhases(evs))
	}
	d := evs[len(evs)-1].Data
	if d["phase"] != "ok" || d["members"] != "2" {
		t.Errorf("payload = %v, want ok with members 2", d)
	}
	if _, ok := d["ms"]; !ok {
		t.Errorf("the end phase must carry ms, got %v", d)
	}
}

// TestGroupOpsSilentWhenTelemetryOff: with telemetry.enabled = false the chain
// runs and records nothing at all — no file, no op.
func TestGroupOpsSilentWhenTelemetryOff(t *testing.T) {
	roots := peekFixture(t, "origin", "api", "ui")
	storeGroup(t, "web", roots[1:])
	// The setting is read live off the global config, which the hops reload
	// from disk — so the switch has to write it at user scope, not just flip
	// the in-memory copy.
	opts := config.Discover(".")
	if err := config.WriteKey(opts, config.UserScope, "telemetry.enabled", false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.WriteKey(opts, config.UserScope, "telemetry.enabled", true) })
	cfg, _ := config.Load(opts)
	prev := config.Get()
	config.Set(cfg)
	t.Cleanup(func() { config.Set(prev) })

	m := switchModel(t)
	m, _ = openGroup(t, m, "web")

	if evs := groupOps(t, m, telemetry.OpProjectGroupOpen); len(evs) != 0 {
		t.Fatalf("telemetry.enabled = false must record nothing, got %v", evs)
	}
}

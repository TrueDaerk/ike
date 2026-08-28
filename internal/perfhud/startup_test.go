package perfhud

import (
	"strings"
	"testing"
	"time"
)

// TestStartupPhasesRecordAndReplace checks phases append in completion order
// and a re-recorded name (a project switch) replaces in place.
func TestStartupPhasesRecordAndReplace(t *testing.T) {
	t.Cleanup(ResetStartup)
	StartupBegin(time.Now())
	RecordStartupPhase("config", 5*time.Millisecond)
	RecordStartupPhase("session-restore", 8*time.Millisecond)
	RecordStartupPhase("config", 3*time.Millisecond)

	phases, first := StartupPhases()
	if first != 0 {
		t.Fatalf("first frame recorded before any frame: %v", first)
	}
	if len(phases) != 2 {
		t.Fatalf("want 2 phases, got %v", phases)
	}
	if phases[0].Name != "config" || phases[0].D != 3*time.Millisecond {
		t.Fatalf("re-record did not replace in place: %v", phases[0])
	}
	if phases[1].Name != "session-restore" {
		t.Fatalf("completion order lost: %v", phases)
	}
}

// TestRecordFirstFrameFiresOnce pins the one-time semantics the startup log
// line depends on, and the no-op without a StartupBegin (every app test).
func TestRecordFirstFrameFiresOnce(t *testing.T) {
	t.Cleanup(ResetStartup)
	if _, ok := RecordFirstFrame(time.Now()); ok {
		t.Fatal("first frame must not record without StartupBegin")
	}
	begin := time.Now()
	StartupBegin(begin)
	d, ok := RecordFirstFrame(begin.Add(120 * time.Millisecond))
	if !ok || d != 120*time.Millisecond {
		t.Fatalf("first RecordFirstFrame = (%v, %v)", d, ok)
	}
	if _, ok := RecordFirstFrame(begin.Add(time.Second)); ok {
		t.Fatal("second RecordFirstFrame must not re-record")
	}
	if _, first := StartupPhases(); first != 120*time.Millisecond {
		t.Fatalf("stored first frame = %v", first)
	}
}

// TestStartupSectionRenders checks both consumers carry the section: the HUD
// body lists the costliest phases under the first-frame line, the snapshot
// carries every phase.
func TestStartupSectionRenders(t *testing.T) {
	t.Cleanup(ResetStartup)
	begin := time.Now()
	StartupBegin(begin)
	RecordStartupPhase("wasm-plugins", 40*time.Millisecond)
	RecordStartupPhase("model-build", 200*time.Millisecond)
	RecordStartupPhase("config", 2*time.Millisecond)
	RecordStartupPhase("cli-open", time.Millisecond)
	RecordFirstFrame(begin.Add(300 * time.Millisecond))

	lines := startupLines(2)
	if len(lines) != 3 {
		t.Fatalf("want first-frame line + 2 phases, got %v", lines)
	}
	if !strings.Contains(lines[0], "first frame 300.0ms") {
		t.Fatalf("first-frame line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "model-build") || !strings.Contains(lines[2], "wasm-plugins") {
		t.Fatalf("costliest phases not first: %v", lines)
	}

	snap := SnapshotText(Sample{Goroutines: 1}, nil)
	for _, want := range []string{"first frame in 300.0ms", "wasm-plugins", "model-build", "config", "cli-open"} {
		if !strings.Contains(snap, want) {
			t.Fatalf("snapshot missing %q:\n%s", want, snap)
		}
	}
}

// TestStartupSectionAbsentWhenUnrecorded keeps the HUD and snapshot free of a
// fictional startup block in processes that never measured one.
func TestStartupSectionAbsentWhenUnrecorded(t *testing.T) {
	t.Cleanup(ResetStartup)
	ResetStartup()
	if lines := startupLines(3); lines != nil {
		t.Fatalf("unexpected startup lines: %v", lines)
	}
	if snap := SnapshotText(Sample{Goroutines: 1}, nil); strings.Contains(snap, "startup") {
		t.Fatalf("snapshot carries a startup block without data:\n%s", snap)
	}
}

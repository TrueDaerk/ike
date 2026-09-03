package telemetry

import (
	"strconv"
	"testing"
	"time"
)

// optimer_test.go covers the shared op timing helper (#2403).

// TestOpTimerBracketsWithDuration: the timer emits the start phase itself and
// its closer stamps a non-negative ms into the end phase, merged with the
// caller's structural detail.
func TestOpTimerBracketsWithDuration(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	end := r.OpTimer(OpProjectSwitch)
	end("ok", map[string]string{"parked": "true", "panes": "4"})
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 2 {
		t.Fatalf("want a start and an end event, got %v", evs)
	}
	if evs[0].Type != TypeOp || evs[0].Data["id"] != OpProjectSwitch || evs[0].Data["phase"] != "start" {
		t.Fatalf("start event wrong: %v", evs[0])
	}
	if _, ok := evs[0].Data["ms"]; ok {
		t.Errorf("start event carries a duration: %v", evs[0])
	}
	got := evs[1]
	if got.Data["phase"] != "ok" || got.Data["parked"] != "true" || got.Data["panes"] != "4" {
		t.Fatalf("end event wrong: %v", got)
	}
	if ms, err := strconv.ParseInt(got.Data["ms"], 10, 64); err != nil || ms < 0 {
		t.Errorf("ms = %q (%v), want a non-negative number", got.Data["ms"], err)
	}
}

// TestOpTimerUsesTheInjectedClock: the helper measures with the recorder's own
// clock, so a test can hand it a fixed elapsed time.
func TestOpTimerUsesTheInjectedClock(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	steps := []time.Time{base, base.Add(1500 * time.Millisecond), base.Add(1500 * time.Millisecond)}
	i := 0
	r.now = func() time.Time {
		if i < len(steps) {
			i++
		}
		return steps[i-1]
	}
	end := r.OpTimer(OpProjectSwitch)
	end("ok", nil)
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 2 || evs[1].Data["ms"] != "1500" {
		t.Fatalf("ms = %v, want 1500", evs)
	}
}

// TestOpTimerOnNilRecorderIsSafe: telemetry-less models (tests, a discarded
// switch model) call the same seam.
func TestOpTimerOnNilRecorderIsSafe(t *testing.T) {
	var r *Recorder
	end := r.OpTimer(OpProjectSwitch)
	end("ok", map[string]string{"panes": "1"})
}

// TestSessionRestoreOpLeavesNoGhostFile: the startup restore runs on every
// launch, so it must not create a session file on its own (#2403) — the same
// rule the deferred session marker and pane.focus follow.
func TestSessionRestoreOpLeavesNoGhostFile(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	end := r.OpTimer(OpSessionRestore)
	end("ok", nil)
	r.Close()

	if files := sessionFiles(t, dir); len(files) != 0 {
		t.Fatalf("restore-only launch left a session file: %v", files)
	}
}

// TestSessionRestoreOpLandsInARealSession: held, not dropped — a launch that
// goes on to do something writes the restore span too.
func TestSessionRestoreOpLandsInARealSession(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	end := r.OpTimer(OpSessionRestore)
	end("ok", nil)
	r.Command("file.open", SourcePalette)
	r.Close()

	var restores int
	for _, ev := range readSession(t, dir) {
		if ev.Type == TypeOp && ev.Data["id"] == OpSessionRestore {
			restores++
		}
	}
	if restores != 2 {
		t.Fatalf("want the held start/ok pair in the file, got %d", restores)
	}
}

package app

import (
	"testing"
	"time"
)

// #2163: a past-due debounce mark that the tick handler does not consume is a
// zero-delay tick loop — armBackupTick/armAutosaveIdleTick clamp a negative
// wait to 0, so an unconsumed deadline re-fires every Update pass at 100%
// CPU. Due() is the only thing that clears deadlines, so the handlers must
// call it even when the feature currently reads disabled. These tests pin
// the invariant: after one handler pass, no past-due mark can remain armed.

func TestBackupTickAlwaysDrainsDueMarks(t *testing.T) {
	m := newSized()
	m.backupDeb.Mark("gone-buffer", time.Now().Add(-time.Hour))
	_ = m.snapshotDueBackups(time.Now())
	if cmd := m.armBackupTick(); cmd != nil {
		t.Fatal("past-due mark survived snapshotDueBackups — this is the zero-delay tick loop")
	}
}

func TestAutosaveIdleTickAlwaysDrainsDueMarks(t *testing.T) {
	m := newSized()
	m.autosaveIdleDeb.Mark("gone-buffer", time.Now().Add(-time.Hour))
	m.saveDueIdleBuffers(time.Now())
	if cmd := m.armAutosaveIdleTick(); cmd != nil {
		t.Fatal("past-due mark survived saveDueIdleBuffers — this is the zero-delay tick loop")
	}
}

package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/diag"
)

// TestUpdateAndViewBeatTheWatchdog pins the #2163 wiring: every Update
// dispatch and every View composition must complete a watchdog pass —
// without the heartbeat, the stall monitor is blind and a frozen loop leaves
// no dump.
func TestUpdateAndViewBeatTheWatchdog(t *testing.T) {
	m := newSized()
	before := diag.LoopPasses()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	afterUpdate := diag.LoopPasses()
	if afterUpdate != before+1 {
		t.Fatalf("Update completed %d watchdog passes, want 1", afterUpdate-before)
	}
	tm.(Model).View()
	if got := diag.LoopPasses(); got != afterUpdate+1 {
		t.Fatalf("View completed %d watchdog passes, want 1", got-afterUpdate)
	}
}

// TestWatchdogDefaultOn documents the opt-out contract (#2163): the watchdog
// ships enabled with a sane threshold; perf.watchdog_seconds = 0 is the
// opt-out, and validation clamps nonsense.
func TestWatchdogDefaultOn(t *testing.T) {
	cfg, _ := config.Load(config.Options{})
	if cfg.Perf.WatchdogSeconds <= 0 {
		t.Fatalf("watchdog must be on by default, got threshold %d", cfg.Perf.WatchdogSeconds)
	}
}

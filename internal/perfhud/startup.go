package perfhud

import (
	"sync"
	"time"
)

// startup.go records the startup / project-open phase timings (#2260): main.go
// and the model constructor stamp each phase as it finishes, and the first
// composed frame closes the sequence. Unlike the rest of the collector this is
// not gated on Enabled — startup is over before the HUD can be toggled on, so
// the phases are recorded unconditionally (a handful of appends, nothing
// periodic) and shown as a static block whenever the HUD or snapshot is read.
//
// A project switch re-records the phases it re-runs: RecordStartupPhase
// replaces an existing entry by name, so the block always describes the most
// recent open of each phase rather than accumulating duplicates.

// StartupPhase is one named startup step and its wall-clock cost.
type StartupPhase struct {
	Name string
	D    time.Duration
}

var startupState struct {
	mu         sync.Mutex
	begin      time.Time // process start; zero until StartupBegin
	phases     []StartupPhase
	firstFrame time.Duration // begin → first real frame; 0 = not yet composed
}

// StartupBegin marks the process start the first-frame duration is measured
// from. main.go calls it first thing; tests inject their own instant.
func StartupBegin(t time.Time) {
	startupState.mu.Lock()
	defer startupState.mu.Unlock()
	startupState.begin = t
	startupState.phases = nil
	startupState.firstFrame = 0
}

// RecordStartupPhase records one finished startup phase. A phase recorded
// before under the same name is replaced in place (a project switch re-runs
// the constructor phases); a new name appends in completion order.
func RecordStartupPhase(name string, d time.Duration) {
	startupState.mu.Lock()
	defer startupState.mu.Unlock()
	for i := range startupState.phases {
		if startupState.phases[i].Name == name {
			startupState.phases[i].D = d
			return
		}
	}
	startupState.phases = append(startupState.phases, StartupPhase{Name: name, D: d})
}

// RecordFirstFrame closes the startup measurement at the first composed frame.
// It reports the duration once: ok is true only on the call that recorded it,
// so the caller can emit its one-time startup log line. Without a prior
// StartupBegin (tests, embedded models) it is a no-op.
func RecordFirstFrame(now time.Time) (time.Duration, bool) {
	startupState.mu.Lock()
	defer startupState.mu.Unlock()
	if startupState.begin.IsZero() || startupState.firstFrame != 0 {
		return 0, false
	}
	d := now.Sub(startupState.begin)
	if d <= 0 {
		d = time.Nanosecond // a degenerate clock still counts as recorded
	}
	startupState.firstFrame = d
	return d, true
}

// StartupPhases returns the recorded phases (completion order) and the
// first-frame duration (0 while no frame has been composed yet).
func StartupPhases() ([]StartupPhase, time.Duration) {
	startupState.mu.Lock()
	defer startupState.mu.Unlock()
	out := make([]StartupPhase, len(startupState.phases))
	copy(out, startupState.phases)
	return out, startupState.firstFrame
}

// ResetStartup clears the recorded startup state (tests).
func ResetStartup() {
	StartupBegin(time.Time{})
}

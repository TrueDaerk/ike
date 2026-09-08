package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/backup"
)

// debouncetick.go holds the wake-up scheduler the per-buffer debounces share
// (#2463): the crash-recovery snapshots (#210), the idle autosave (#731) and
// the .http variable lint (#2194) all mark buffers in a backup.Debouncer and
// need exactly one armed timer at the earliest pending deadline.

// debounceTick builds the wake timer armTick returns. It is tea.Tick in the
// program; the test binary replaces it (app_test.go's TestMain) with a command
// resolving to nil at once. Since #2541 every keystroke into a dirty buffer
// arms these debounces inside its own Update pass — the sync no longer
// travels through a loop the tests never run — and the test helpers drain a
// key's command batch synchronously, so a real 2s snapshot timer would sleep
// out its deadline per typed key and then fire an autosave into a fixture
// that expects its buffer still dirty.
var debounceTick = tea.Tick

// armTick schedules one wake at deb's earliest pending deadline and flips
// armed. An already armed side (or an empty debouncer) schedules nothing: the
// tick handler clears armed and re-arms while marks remain, so a burst of
// edits never stacks timers. The tick message carries the model generation the
// timer was armed in (#2194), which is how a superseded model's wake is
// dropped.
func (m *Model) armTick(armed *bool, deb *backup.Debouncer, mk func(gen int64) tea.Msg) tea.Cmd {
	if *armed || deb == nil {
		return nil
	}
	next, ok := deb.Next()
	if !ok {
		return nil
	}
	*armed = true
	d := time.Until(next)
	if d < 0 {
		d = 0
	}
	gen := m.modelGen
	return debounceTick(d, func(time.Time) tea.Msg { return mk(gen) })
}

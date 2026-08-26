package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"
)

// watchdog.go is the update-loop stall watchdog (#2163). The SIGUSR1/pprof
// hooks above only help when someone is around to send the signal; a session
// that freezes overnight leaves nothing. The watchdog is the always-on
// complement: the bubbletea loop stamps LoopEnter/LoopExit around every
// Update and View pass, and a monitor goroutine — which by construction can
// never be the blocked one — dumps every goroutine's stack to the state dir
// when a single pass has been in flight longer than the configured threshold.
// One dump per stall episode, a session-wide cap against a flapping stall
// filling the disk, and a follow-up log line when the stall resolves, so even
// a self-healing freeze stays attributable after the fact.

// maxWatchdogDumps caps dump files per process: a stall that comes and goes
// (a livelock that yields every few seconds) would otherwise write a file per
// episode for as long as the session lives.
const maxWatchdogDumps = 10

var wd struct {
	// depth counts nested LoopEnter calls on the loop goroutine; >0 means a
	// pass is in flight. enterNanos/exitNanos stamp the outermost transitions.
	// passes counts completed outermost passes (a liveness signal for tests).
	depth      atomic.Int64
	enterNanos atomic.Int64
	exitNanos  atomic.Int64
	passes     atomic.Uint64
	// thresholdNanos is the stall threshold; 0 disables the monitor's checks.
	thresholdNanos atomic.Int64

	// mu guards the fields the monitor reads rarely and the loop writes per
	// pass (what) or reconfiguration writes (dir, logf). Never held while
	// dumping, so a slow disk cannot back-pressure LoopEnter.
	mu   sync.Mutex
	what any            // what the in-flight pass is handling (tea.Msg or label)
	dir  func() string  // state dir for dump files, resolved at dump time
	logf func(string)   // best-effort diagnostic logger (app's debug.log)

	once sync.Once // monitor goroutine spawn
}

// LoopEnter marks the start of an update-loop pass (an Update dispatch or a
// View composition). what names the work for the dump header — the tea.Msg
// being handled, or a label like "render". Nested calls are counted, only the
// outermost stamps the clock.
func LoopEnter(what any) {
	if wd.depth.Add(1) == 1 {
		wd.enterNanos.Store(time.Now().UnixNano())
		wd.mu.Lock()
		wd.what = what
		wd.mu.Unlock()
	}
}

// LoopExit marks the end of the pass opened by the matching LoopEnter.
func LoopExit() {
	if wd.depth.Add(-1) == 0 {
		wd.exitNanos.Store(time.Now().UnixNano())
		wd.passes.Add(1)
	}
}

// LoopPasses returns the number of completed update-loop passes — the
// wiring's regression-test seam.
func LoopPasses() uint64 { return wd.passes.Load() }

// ConfigureWatchdog (re)configures the stall watchdog: seconds is the stall
// threshold (0 disables), dir resolves the directory dump files land in
// (resolved at dump time, so a project switch is followed), logf receives
// one-line diagnostics (best-effort, must not depend on the update loop).
// The monitor goroutine starts on the first enabling call and is shared for
// the process lifetime; later calls just update the parameters.
func ConfigureWatchdog(seconds int, dir func() string, logf func(string)) {
	if seconds < 0 {
		seconds = 0
	}
	wd.mu.Lock()
	wd.dir = dir
	wd.logf = logf
	wd.mu.Unlock()
	wd.thresholdNanos.Store(int64(seconds) * int64(time.Second))
	if seconds > 0 {
		wd.once.Do(func() { go watchdogMonitor() })
	}
}

// watchdogMonitor is the watcher goroutine: it polls the loop stamps and
// dumps when a pass overstays the threshold. Polling (instead of a per-pass
// timer) keeps LoopEnter/LoopExit down to two atomic ops — the hot path pays
// nothing for the diagnosis.
func watchdogMonitor() {
	var dumpedEnter int64 // enterNanos of the episode already dumped
	var dumps int
	for {
		th := wd.thresholdNanos.Load()
		time.Sleep(watchdogCheckInterval(th))
		if th == 0 {
			continue
		}
		if wd.depth.Load() > 0 {
			ent := wd.enterNanos.Load()
			stalled := time.Duration(time.Now().UnixNano() - ent)
			if stalled >= time.Duration(th) && ent != dumpedEnter {
				dumpedEnter = ent
				dumps++
				reportStall(stalled, dumps)
			}
		} else if dumpedEnter != 0 {
			// The dumped episode ended on its own — record how long the loop
			// was actually gone, so "it froze for a minute and came back" is
			// distinguishable from a hard hang in the log.
			total := time.Duration(wd.exitNanos.Load() - dumpedEnter)
			wdLog(fmt.Sprintf("watchdog: update loop recovered after %s", total.Round(time.Millisecond)))
			dumpedEnter = 0
		}
	}
}

// watchdogCheckInterval derives the poll cadence from the threshold: fine
// enough to catch a stall soon after it crosses the line, coarse enough that
// the monitor is invisible. Disabled watchdogs idle at the 1s ceiling.
func watchdogCheckInterval(thresholdNanos int64) time.Duration {
	iv := time.Duration(thresholdNanos) / 4
	if iv < 50*time.Millisecond || thresholdNanos == 0 {
		iv = 50 * time.Millisecond
	}
	if iv > time.Second || thresholdNanos == 0 {
		iv = time.Second
	}
	return iv
}

// reportStall writes the goroutine dump for a detected stall and logs where
// it went. Past maxWatchdogDumps it only logs — the stall is still recorded,
// the disk is not.
func reportStall(stalled time.Duration, nth int) {
	wd.mu.Lock()
	what := wd.what
	dir := wd.dir
	wd.mu.Unlock()
	label := fmt.Sprintf("%T", what)
	if s, ok := what.(string); ok {
		label = s
	}
	if nth > maxWatchdogDumps {
		wdLog(fmt.Sprintf("watchdog: update loop stalled %s in %s (dump cap reached)", stalled.Round(time.Millisecond), label))
		return
	}
	d := os.TempDir()
	if dir != nil {
		if got := dir(); got != "" {
			d = got
		}
	}
	path, err := writeStallDump(d, stalled, label)
	if err != nil {
		wdLog(fmt.Sprintf("watchdog: update loop stalled %s in %s; dump failed: %v", stalled.Round(time.Millisecond), label, err))
		return
	}
	wdLog(fmt.Sprintf("watchdog: update loop stalled %s in %s; goroutine dump: %s", stalled.Round(time.Millisecond), label, path))
}

// writeStallDump writes every goroutine's full stack (pprof debug=2, the
// variant that shows blocking state and wait durations) plus a one-line
// header naming the stalled pass.
func writeStallDump(dir string, stalled time.Duration, label string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("ike-watchdog-%d-%s-goroutines.txt", os.Getpid(), time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fmt.Fprintf(f, "ike update-loop stall: %s in flight for %s (dump written %s)\n\n",
		label, stalled.Round(time.Millisecond), time.Now().Format(time.RFC3339))
	if err := pprof.Lookup("goroutine").WriteTo(f, 2); err != nil {
		return "", err
	}
	return path, nil
}

// wdLog forwards to the configured logger, if any.
func wdLog(line string) {
	wd.mu.Lock()
	logf := wd.logf
	wd.mu.Unlock()
	if logf != nil {
		logf(line)
	}
}

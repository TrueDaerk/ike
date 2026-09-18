package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

// freeze.go is the starvation watch (#2627), the complement to the stall
// watchdog in watchdog.go. The watchdog catches a single Update/View pass
// that overstays its welcome — it only ever sees a pass that *entered*. The
// telemetry heartbeats (#2348) recorded a different failure: the pass count
// standing still for a whole 60 s interval, i.e. the loop never entered a
// pass at all (blocked before LoopEnter, starved of messages, or stuck in the
// runtime). Those logs say *that* the loop went quiet and nothing about
// where, and the episode is long gone by the time anyone reads them.
//
// FreezeWatch runs off the heartbeat goroutine — which by construction is not
// the loop — diffs the pass counter between beats and, on the first frozen
// beat of an episode, writes every goroutine's stack next to debug.log. One
// dump per episode plus a session-wide cap keep a flapping freeze from
// filling the disk; the telemetry side records a `freeze` event per frozen
// beat, so dump and usage log correlate.

// FreezePassThreshold is the number of completed update-loop passes a beat
// interval must carry to count as alive. Below it the interval is reported
// frozen.
//
// Why 3 and not 1: an idle IKE is not a silent IKE — the status bar's clock
// segment, the backup debounce and the VCS/forge polls each wake the loop on
// their own timers, so a minute of genuine idling still completes passes. The
// beats on record froze at 0 passes and at 4 passes across two minutes. A
// small non-zero threshold therefore also catches the "almost nothing moved"
// case a zero test would miss, at the price of an occasional benign dump from
// a session that really did nothing for a minute — cheap, capped, and easy to
// recognize: a benign dump shows the loop parked in its own select.
const FreezePassThreshold = 3

// maxFreezeDumps caps freeze dumps per process, mirroring maxWatchdogDumps: a
// session that keeps flapping in and out of a frozen minute must not write a
// megabyte-sized file every minute for the rest of the day.
const maxFreezeDumps = 3

// freezeStackLimit caps the goroutine dump. runtime.Stack truncates to what
// fits, so a pathological goroutine count costs a bounded 1 MiB instead of
// whatever the process happens to hold.
const freezeStackLimit = 1 << 20

// FreezeReport is one beat's verdict.
type FreezeReport struct {
	Frozen bool          // the interval completed fewer than FreezePassThreshold passes
	First  bool          // the first frozen beat of this episode (the one that dumps)
	Passes uint64        // completed passes in the interval
	Since  time.Duration // wall time since the previous beat
	Dumped bool          // a goroutine dump was written for this beat
}

// FreezeWatch diffs the update-loop pass counter across heartbeats. One
// instance per session, driven exclusively from the heartbeat goroutine
// (Beat) — never from the update loop, which is the thing being diagnosed.
type FreezeWatch struct {
	passes func() uint64 // cumulative completed passes (diag.LoopPasses)
	dir    func() string // directory dump files land in, resolved at dump time
	logf   func(string)  // best-effort diagnostic logger (the app's debug.log)
	now    func() time.Time
	stack  func([]byte, bool) int // runtime.Stack seam for tests

	mu      sync.Mutex
	primed  bool      // a first beat established the baseline
	last    uint64    // pass counter at the previous beat
	lastAt  time.Time // wall clock of the previous beat
	episode bool      // a frozen episode is open (its dump is already written)
	dumps   int       // dumps written this session

	wg sync.WaitGroup // in-flight dump writes (WaitDumps)
}

// NewFreezeWatch builds a watch over the given pass counter (nil means the
// process-wide LoopPasses). dir resolves the dump directory at dump time (so
// a project switch is followed), logf takes one-line diagnostics. Both may be
// nil: the dump then lands in os.TempDir and the line goes nowhere.
func NewFreezeWatch(passes func() uint64, dir func() string, logf func(string)) *FreezeWatch {
	if passes == nil {
		passes = LoopPasses
	}
	return &FreezeWatch{
		passes: passes,
		dir:    dir,
		logf:   logf,
		now:    time.Now,
		stack:  runtime.Stack,
	}
}

// Beat records one heartbeat and reports whether the interval that just ended
// looks frozen. snapshot is the heartbeat payload; it goes into the dump
// header, so the file and the telemetry beat carry the same numbers. The very
// first beat only establishes the baseline and is never frozen.
//
// A frozen beat that opens an episode starts the dump in its own goroutine
// and returns immediately: the heartbeat must keep beating even while a slow
// disk swallows a megabyte of stacks, and a blocked update loop can never
// hold the write up — nothing on this path touches the loop.
func (w *FreezeWatch) Beat(snapshot map[string]string) FreezeReport {
	if w == nil {
		return FreezeReport{}
	}
	cur := w.passes()
	now := w.now()

	w.mu.Lock()
	if !w.primed {
		w.primed, w.last, w.lastAt = true, cur, now
		w.mu.Unlock()
		return FreezeReport{}
	}
	rep := FreezeReport{
		Passes: cur - w.last,
		Since:  now.Sub(w.lastAt),
	}
	w.last, w.lastAt = cur, now
	if rep.Passes >= FreezePassThreshold {
		w.episode = false
		w.mu.Unlock()
		return rep
	}
	rep.Frozen = true
	rep.First = !w.episode
	w.episode = true
	dump, nth := rep.First && w.dumps < maxFreezeDumps, 0
	if dump {
		w.dumps++
		nth = w.dumps
		rep.Dumped = true
	}
	w.mu.Unlock()

	if rep.First && !dump {
		w.log(fmt.Sprintf("freeze: update loop completed %d passes in %s (dump cap reached)",
			rep.Passes, rep.Since.Round(time.Millisecond)))
	}
	if dump {
		header := freezeHeader(rep, snapshot)
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.writeDump(header, nth)
		}()
	}
	return rep
}

// freezeHeader renders the dump's first lines: the verdict plus the heartbeat
// snapshot that produced it, key-sorted so two dumps diff cleanly.
func freezeHeader(rep FreezeReport, snapshot map[string]string) string {
	keys := make([]string, 0, len(snapshot))
	for k := range snapshot {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := fmt.Sprintf("ike update-loop freeze: %d passes in %s (threshold %d, dump written %s)\nheartbeat:",
		rep.Passes, rep.Since.Round(time.Millisecond), FreezePassThreshold, time.Now().Format(time.RFC3339))
	for _, k := range keys {
		s += " " + k + "=" + snapshot[k]
	}
	return s + "\n\n"
}

// writeDump writes the header plus every goroutine's stack to a sibling of
// debug.log and logs where it went. Best-effort throughout: a diagnostic that
// fails must stay silent about everything but its log line.
func (w *FreezeWatch) writeDump(header string, nth int) {
	buf := make([]byte, freezeStackLimit)
	n := w.stack(buf, true)

	d := os.TempDir()
	if w.dir != nil {
		if got := w.dir(); got != "" {
			d = got
		}
	}
	path, err := writeFreezeDump(d, nth, header, buf[:n])
	if err != nil {
		w.log(fmt.Sprintf("freeze: goroutine dump failed: %v", err))
		return
	}
	w.log("freeze: update loop quiet across a heartbeat interval; goroutine dump: " + path)
}

// writeFreezeDump creates the dump file and fills it. nth is the dump's
// per-session ordinal — part of the name, so two episodes inside the same
// second cannot land on the same file.
func writeFreezeDump(dir string, nth int, header string, stacks []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("ike-freeze-%d-%s-%d-goroutines.txt", os.Getpid(), time.Now().Format("20060102-150405"), nth)
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(header); err != nil {
		return "", err
	}
	if _, err := f.Write(stacks); err != nil {
		return "", err
	}
	return path, nil
}

// log forwards a line to the configured logger, if any.
func (w *FreezeWatch) log(line string) {
	if w.logf != nil {
		w.logf(line)
	}
}

// WaitDumps blocks until every dump started so far has been written. Only
// tests need it — the session itself never waits on a diagnostic.
func (w *FreezeWatch) WaitDumps() {
	if w != nil {
		w.wg.Wait()
	}
}

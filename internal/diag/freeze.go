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
//
// A standing pass counter alone is not a freeze (#2692). Since the idle-churn
// work (#2540, #2626) removed most periodic wake-ups, a quiet minute over an
// idle loop looks exactly like a frozen one from the counter's side, and the
// three dumps the wild produced all showed the loop parked in its own select,
// with nothing to do. The verdict therefore needs a second signal: a pass in
// flight at the beat (the stall watchdog's own depth counter, so both
// diagnostics agree on what "in a pass" means), or input that reached the
// program during the interval without a pass completing. A quiet interval
// with an idle loop is no longer frozen — it simply had no work.

// FreezePassThreshold is the number of completed update-loop passes a beat
// interval must carry to count as alive. Below it the interval is a
// *candidate* for frozen — the verdict then needs evidence that the loop
// owed someone a pass (see Beat).
//
// Why 3 and not 1: an idle IKE was never a silent IKE — the status bar's
// clock segment, the backup debounce and the VCS/forge polls each woke the
// loop on their own timers, so a minute of genuine idling still completed
// passes. The beats on record froze at 0 passes and at 4 passes across two
// minutes. A small non-zero threshold therefore also catches the "almost
// nothing moved" case a zero test would miss.
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
	Frozen   bool          // too few passes AND work was pending (in flight or input)
	First    bool          // the first frozen beat of this episode (the one that dumps)
	Passes   uint64        // completed passes in the interval
	Since    time.Duration // wall time since the previous beat
	Dumped   bool          // a goroutine dump was written for this beat
	InFlight bool          // a pass was in flight when the beat was taken
	Inputs   uint64        // input messages that reached the program during the interval
}

// FreezeWatch diffs the update-loop pass counter across heartbeats. One
// instance per session, driven exclusively from the heartbeat goroutine
// (Beat) — never from the update loop, which is the thing being diagnosed.
type FreezeWatch struct {
	src   FreezeSources
	dir   func() string // directory dump files land in, resolved at dump time
	logf  func(string)  // best-effort diagnostic logger (the app's debug.log)
	now   func() time.Time
	stack func([]byte, bool) int // runtime.Stack seam for tests

	mu         sync.Mutex
	primed     bool      // a first beat established the baseline
	last       uint64    // pass counter at the previous beat
	lastInputs uint64    // input counter at the previous beat
	lastAt     time.Time // wall clock of the previous beat
	episode    bool      // a frozen episode is open (its dump is already written)
	dumps      int       // dumps written this session

	wg sync.WaitGroup // in-flight dump writes (WaitDumps)
}

// FreezeSources are the three signals a beat reads: how many passes the loop
// has completed, whether one is in flight right now, and how much input has
// reached the program. Every field may be nil and then falls back to the
// process-wide counter — tests substitute all three.
type FreezeSources struct {
	Passes   func() uint64 // cumulative completed passes (LoopPasses)
	InFlight func() bool   // a pass is running right now (LoopInFlight)
	Inputs   func() uint64 // cumulative input messages (InputCount)
}

// withDefaults fills the nil seams with the process-wide counters.
func (s FreezeSources) withDefaults() FreezeSources {
	if s.Passes == nil {
		s.Passes = LoopPasses
	}
	if s.InFlight == nil {
		s.InFlight = LoopInFlight
	}
	if s.Inputs == nil {
		s.Inputs = InputCount
	}
	return s
}

// NewFreezeWatch builds a watch over the given signal sources (see
// FreezeSources; the zero value reads the process-wide counters). dir
// resolves the dump directory at dump time (so a project switch is
// followed), logf takes one-line diagnostics. Both may be nil: the dump then
// lands in os.TempDir and the line goes nowhere.
func NewFreezeWatch(src FreezeSources, dir func() string, logf func(string)) *FreezeWatch {
	return &FreezeWatch{
		src:   src.withDefaults(),
		dir:   dir,
		logf:  logf,
		now:   time.Now,
		stack: runtime.Stack,
	}
}

// Beat records one heartbeat and reports whether the interval that just ended
// was frozen: too few completed passes AND work the loop owed someone — a
// pass in flight at the beat, or input that arrived during the interval
// without a pass completing (#2692). snapshot is the heartbeat payload; it
// goes into the dump header, so the file and the telemetry beat carry the
// same numbers. The very first beat only establishes the baseline and is
// never frozen.
//
// A frozen beat that opens an episode starts the dump in its own goroutine
// and returns immediately: the heartbeat must keep beating even while a slow
// disk swallows a megabyte of stacks, and a blocked update loop can never
// hold the write up — nothing on this path touches the loop.
func (w *FreezeWatch) Beat(snapshot map[string]string) FreezeReport {
	if w == nil {
		return FreezeReport{}
	}
	cur := w.src.Passes()
	curInputs := w.src.Inputs()
	inFlight := w.src.InFlight()
	now := w.now()

	w.mu.Lock()
	if !w.primed {
		w.primed, w.last, w.lastInputs, w.lastAt = true, cur, curInputs, now
		w.mu.Unlock()
		return FreezeReport{}
	}
	rep := FreezeReport{
		Passes:   cur - w.last,
		Since:    now.Sub(w.lastAt),
		InFlight: inFlight,
		Inputs:   curInputs - w.lastInputs,
	}
	w.last, w.lastInputs, w.lastAt = cur, curInputs, now
	if rep.Passes >= FreezePassThreshold {
		w.episode = false
		w.mu.Unlock()
		return rep
	}
	if !rep.InFlight && rep.Inputs == 0 {
		// An idle loop, not a frozen one: nothing was running and nobody was
		// waiting for an answer. Closes any open episode — the next beat that
		// does find work pending is a fresh one, worth its own dump.
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
		w.log(fmt.Sprintf("freeze: update loop completed %d passes in %s, %s (dump cap reached)",
			rep.Passes, rep.Since.Round(time.Millisecond), freezeReason(rep)))
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
	s := fmt.Sprintf("ike update-loop freeze: %d passes in %s, %s (threshold %d, dump written %s)\nheartbeat:",
		rep.Passes, rep.Since.Round(time.Millisecond), freezeReason(rep), FreezePassThreshold,
		time.Now().Format(time.RFC3339))
	for _, k := range keys {
		s += " " + k + "=" + snapshot[k]
	}
	return s + "\n\n"
}

// freezeReason names the evidence that made the beat frozen — the part a
// reader needs to pick the right stacks out of the dump: a pass that never
// returned, or input nobody answered.
func freezeReason(rep FreezeReport) string {
	switch {
	case rep.InFlight && rep.Inputs > 0:
		return fmt.Sprintf("a pass in flight and %d input messages pending", rep.Inputs)
	case rep.InFlight:
		return "a pass in flight"
	default:
		return fmt.Sprintf("%d input messages pending", rep.Inputs)
	}
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

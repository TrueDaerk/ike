package diag

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testWatch is a FreezeWatch over a fake clock and fake signal sources: the
// pass counter, the "a pass is in flight" flag and the input counter are all
// driven from the test. inFlight starts true — a stuck loop is what the
// episode/dump tests are about; the idle-loop tests clear it.
type testWatch struct {
	*FreezeWatch
	mu     sync.Mutex
	logged []string
}

// newTestWatch builds a watch writing its dumps into dir. The returned funcs
// advance the pass counter, park/unpark the loop and enqueue input; the clock
// advances one beat interval per Beat.
func newTestWatch(t *testing.T, dir string) (tw *testWatch, addPasses func(uint64), setInFlight func(bool), addInput func(uint64)) {
	t.Helper()
	var mu sync.Mutex
	var passes, inputs uint64
	inFlight := true
	tw = &testWatch{}
	tw.FreezeWatch = NewFreezeWatch(
		FreezeSources{
			Passes: func() uint64 {
				mu.Lock()
				defer mu.Unlock()
				return passes
			},
			InFlight: func() bool {
				mu.Lock()
				defer mu.Unlock()
				return inFlight
			},
			Inputs: func() uint64 {
				mu.Lock()
				defer mu.Unlock()
				return inputs
			},
		},
		func() string { return dir },
		func(s string) {
			tw.mu.Lock()
			defer tw.mu.Unlock()
			tw.logged = append(tw.logged, s)
		},
	)
	now := time.Unix(1700000000, 0)
	tw.now = func() time.Time {
		now = now.Add(time.Minute) // one beat interval per call
		return now
	}
	return tw, func(n uint64) {
			mu.Lock()
			defer mu.Unlock()
			passes += n
		}, func(v bool) {
			mu.Lock()
			defer mu.Unlock()
			inFlight = v
		}, func(n uint64) {
			mu.Lock()
			defer mu.Unlock()
			inputs += n
		}
}

// lines returns the diagnostic lines the watch logged so far.
func (tw *testWatch) lines() []string {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return append([]string(nil), tw.logged...)
}

// dumpFiles lists the goroutine dumps written into dir.
func dumpFiles(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dump dir: %v", err)
	}
	var out []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "ike-freeze-") {
			out = append(out, e.Name())
		}
	}
	return out
}

// A standing pass counter is the freeze signature (#2627): the first beat only
// primes the baseline, the second reports frozen and writes the episode's one
// dump, and further frozen beats report without dumping again.
func TestFreezeWatchDumpsOncePerEpisode(t *testing.T) {
	dir := t.TempDir()
	w, _, _, _ := newTestWatch(t, dir)

	if rep := w.Beat(map[string]string{"passes": "10"}); rep.Frozen {
		t.Fatalf("priming beat reported frozen: %+v", rep)
	}
	rep := w.Beat(map[string]string{"passes": "10", "top": "app.tickMsg:1"})
	if !rep.Frozen || !rep.First || !rep.Dumped {
		t.Fatalf("second beat: want frozen/first/dumped, got %+v", rep)
	}
	if rep.Passes != 0 || rep.Since != time.Minute {
		t.Fatalf("second beat payload wrong: %+v", rep)
	}
	w.WaitDumps()
	files := dumpFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want 1 dump file, got %v", files)
	}

	raw, err := os.ReadFile(filepath.Join(dir, files[0]))
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "0 passes in 1m0s, a pass in flight") {
		t.Errorf("dump header misses the verdict: %q", firstLines(body))
	}
	if !strings.Contains(body, "passes=10") || !strings.Contains(body, "top=app.tickMsg:1") {
		t.Errorf("dump header misses the heartbeat snapshot: %q", firstLines(body))
	}
	if !strings.Contains(body, "goroutine ") || !strings.Contains(body, "diag.TestFreezeWatchDumpsOncePerEpisode") {
		t.Errorf("dump carries no goroutine stacks: %q", firstLines(body))
	}

	// A third frozen beat belongs to the same episode: reported, not dumped.
	rep = w.Beat(map[string]string{"passes": "10"})
	if !rep.Frozen || rep.First || rep.Dumped {
		t.Fatalf("third beat: want frozen, not first, not dumped, got %+v", rep)
	}
	w.WaitDumps()
	if files := dumpFiles(t, dir); len(files) != 1 {
		t.Fatalf("episode wrote a second dump: %v", files)
	}
	if lines := w.lines(); len(lines) != 1 || !strings.Contains(lines[0], "goroutine dump: ") {
		t.Errorf("debug-log line wrong: %v", lines)
	}
}

// An advancing counter is a live loop: no verdict, no dump, no event.
func TestFreezeWatchSilentWhileTheLoopRuns(t *testing.T) {
	dir := t.TempDir()
	w, addPasses, _, _ := newTestWatch(t, dir)

	for i := 0; i < 4; i++ {
		addPasses(FreezePassThreshold)
		if rep := w.Beat(map[string]string{"passes": "1"}); rep.Frozen {
			t.Fatalf("beat %d reported frozen: %+v", i, rep)
		}
	}
	w.WaitDumps()
	if files := dumpFiles(t, dir); len(files) != 0 {
		t.Fatalf("live loop wrote dumps: %v", files)
	}
	if lines := w.lines(); len(lines) != 0 {
		t.Fatalf("live loop logged: %v", lines)
	}
}

// A quiet interval over an idle loop is not a freeze (#2692): nothing was in
// flight, nobody had typed, so there was simply nothing to do — no verdict,
// no event, no dump. This is the false positive the three dumps of
// 2026-09-21 were.
func TestFreezeWatchIgnoresAnIdleLoop(t *testing.T) {
	dir := t.TempDir()
	w, _, setInFlight, _ := newTestWatch(t, dir)
	setInFlight(false)

	w.Beat(nil) // prime
	for i := 0; i < 3; i++ {
		rep := w.Beat(map[string]string{"passes": "10"})
		if rep.Frozen || rep.Dumped {
			t.Fatalf("idle beat %d reported frozen: %+v", i, rep)
		}
		if rep.Passes != 0 || rep.Inputs != 0 || rep.InFlight {
			t.Fatalf("idle beat %d payload wrong: %+v", i, rep)
		}
	}
	w.WaitDumps()
	if files := dumpFiles(t, dir); len(files) != 0 {
		t.Fatalf("idle loop wrote dumps: %v", files)
	}
	if lines := w.lines(); len(lines) != 0 {
		t.Fatalf("idle loop logged: %v", lines)
	}
}

// Input that reached the program without a pass completing is the other
// freeze signature (#2692): the loop is not inside a pass, yet a keystroke
// went unanswered for a whole interval.
func TestFreezeWatchReportsPendingInput(t *testing.T) {
	dir := t.TempDir()
	w, _, setInFlight, addInput := newTestWatch(t, dir)
	setInFlight(false)

	w.Beat(nil) // prime
	addInput(2)
	rep := w.Beat(map[string]string{"passes": "10"})
	if !rep.Frozen || !rep.First || !rep.Dumped {
		t.Fatalf("pending input: want a frozen first beat with a dump, got %+v", rep)
	}
	if rep.Inputs != 2 || rep.InFlight {
		t.Fatalf("pending-input payload wrong: %+v", rep)
	}
	w.WaitDumps()
	files := dumpFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want 1 dump file, got %v", files)
	}
	raw, err := os.ReadFile(filepath.Join(dir, files[0]))
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	if !strings.Contains(string(raw), "2 input messages pending") {
		t.Errorf("dump header does not name the reason: %q", firstLines(string(raw)))
	}

	// The input is answered and the loop goes back to sleep: no more verdicts.
	if rep := w.Beat(nil); rep.Frozen {
		t.Fatalf("quiet beat after the input was answered reported frozen: %+v", rep)
	}
}

// A recovered loop re-arms the episode: the next freeze dumps again.
func TestFreezeWatchReArmsAfterRecovery(t *testing.T) {
	dir := t.TempDir()
	w, addPasses, _, _ := newTestWatch(t, dir)

	w.Beat(nil)    // prime
	w.Beat(nil)    // frozen: dump 1
	addPasses(100) //
	if rep := w.Beat(nil); rep.Frozen {
		t.Fatalf("recovery beat reported frozen: %+v", rep)
	}
	if rep := w.Beat(nil); !rep.First || !rep.Dumped {
		t.Fatalf("post-recovery freeze: want a fresh dump, got %+v", rep)
	}
	w.WaitDumps()
	if files := dumpFiles(t, dir); len(files) != 2 {
		t.Fatalf("want 2 dumps across two episodes, got %v", files)
	}
}

// Past the session cap an episode is still reported, but no longer written.
func TestFreezeWatchCapsDumpsPerSession(t *testing.T) {
	dir := t.TempDir()
	w, addPasses, _, _ := newTestWatch(t, dir)

	w.Beat(nil) // prime
	for i := 0; i < maxFreezeDumps+2; i++ {
		rep := w.Beat(nil)
		if !rep.Frozen || !rep.First {
			t.Fatalf("episode %d: want a fresh frozen episode, got %+v", i, rep)
		}
		addPasses(100)
		w.Beat(nil) // recovery, closing the episode
	}
	w.WaitDumps()
	if files := dumpFiles(t, dir); len(files) != maxFreezeDumps {
		t.Fatalf("want %d dumps, got %v", maxFreezeDumps, files)
	}
	var capped int
	lines := w.lines()
	for _, l := range lines {
		if strings.Contains(l, "dump cap reached") {
			capped++
		}
	}
	if capped != 2 {
		t.Fatalf("want 2 cap lines, got %v", lines)
	}
}

// The dump is written off the heartbeat goroutine, so an update loop that is
// wedged — the very thing being diagnosed — cannot hold it up, and Beat
// itself returns while the megabyte of stacks is still going to disk.
func TestFreezeWatchDumpsWhileTheLoopIsBlocked(t *testing.T) {
	dir := t.TempDir()
	w, _, _, _ := newTestWatch(t, dir)
	// The verdict must come from the package's own in-flight signal here: the
	// parked goroutine below is a real pass, held open by LoopEnter.
	w.src.InFlight = LoopInFlight

	// A fake update loop, wedged inside a pass: it holds the barrier for as
	// long as the test wants, exactly like a frozen session's loop.
	release := make(chan struct{})
	loopParked := make(chan struct{})
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		LoopEnter("test/blocked")
		close(loopParked)
		<-release
		LoopExit()
	}()
	<-loopParked
	defer func() {
		close(release)
		<-loopDone // leave the package's pass counters balanced
	}()

	// Beat must not block on the write: gate the stack capture and check that
	// Beat has already returned while the dump goroutine still waits.
	capturing := make(chan struct{})
	finish := make(chan struct{})
	w.stack = func(buf []byte, all bool) int {
		close(capturing)
		<-finish
		return copy(buf, []byte("goroutine 1 [running]:\n"))
	}

	w.Beat(nil) // prime
	done := make(chan FreezeReport, 1)
	go func() { done <- w.Beat(nil) }()

	select {
	case rep := <-done:
		if !rep.Dumped {
			t.Fatalf("frozen beat did not dump: %+v", rep)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Beat blocked on the dump write")
	}
	<-capturing
	if files := dumpFiles(t, dir); len(files) != 0 {
		t.Fatalf("dump file appeared before the capture finished: %v", files)
	}
	close(finish)
	w.WaitDumps()
	if files := dumpFiles(t, dir); len(files) != 1 {
		t.Fatalf("want 1 dump written past the blocked loop, got %v", files)
	}
}

// A nil watch is inert, like every other diagnostic seam here.
func TestFreezeWatchNilIsInert(t *testing.T) {
	var w *FreezeWatch
	if rep := w.Beat(nil); rep.Frozen {
		t.Fatalf("nil watch reported frozen: %+v", rep)
	}
	w.WaitDumps()
}

// firstLines trims a dump body for error messages.
func firstLines(s string) string {
	if len(s) > 400 {
		return s[:400]
	}
	return s
}

package diag

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestWatch builds a FreezeWatch over a fake clock and a fake pass counter
// writing into dir. The returned funcs advance the counter and the clock.
func newTestWatch(t *testing.T, dir string) (w *FreezeWatch, addPasses func(uint64), lines *[]string) {
	t.Helper()
	var mu sync.Mutex
	var passes uint64
	var logged []string
	w = NewFreezeWatch(
		func() uint64 {
			mu.Lock()
			defer mu.Unlock()
			return passes
		},
		func() string { return dir },
		func(s string) {
			mu.Lock()
			defer mu.Unlock()
			logged = append(logged, s)
		},
	)
	now := time.Unix(1700000000, 0)
	w.now = func() time.Time {
		now = now.Add(time.Minute) // one beat interval per call
		return now
	}
	return w, func(n uint64) {
		mu.Lock()
		defer mu.Unlock()
		passes += n
	}, &logged
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
	w, _, logged := newTestWatch(t, dir)

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
	if !strings.Contains(body, "0 passes in 1m0s") {
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
	if len(*logged) != 1 || !strings.Contains((*logged)[0], "goroutine dump: ") {
		t.Errorf("debug-log line wrong: %v", *logged)
	}
}

// An advancing counter is a live loop: no verdict, no dump, no event.
func TestFreezeWatchSilentWhileTheLoopRuns(t *testing.T) {
	dir := t.TempDir()
	w, addPasses, logged := newTestWatch(t, dir)

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
	if len(*logged) != 0 {
		t.Fatalf("live loop logged: %v", *logged)
	}
}

// A recovered loop re-arms the episode: the next freeze dumps again.
func TestFreezeWatchReArmsAfterRecovery(t *testing.T) {
	dir := t.TempDir()
	w, addPasses, _ := newTestWatch(t, dir)

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
	w, addPasses, logged := newTestWatch(t, dir)

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
	for _, l := range *logged {
		if strings.Contains(l, "dump cap reached") {
			capped++
		}
	}
	if capped != 2 {
		t.Fatalf("want 2 cap lines, got %v", *logged)
	}
}

// The dump is written off the heartbeat goroutine, so an update loop that is
// wedged — the very thing being diagnosed — cannot hold it up, and Beat
// itself returns while the megabyte of stacks is still going to disk.
func TestFreezeWatchDumpsWhileTheLoopIsBlocked(t *testing.T) {
	dir := t.TempDir()
	w, _, _ := newTestWatch(t, dir)

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

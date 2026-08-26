package diag

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// collectLog is a threadsafe sink for watchdog log lines.
type collectLog struct {
	mu    sync.Mutex
	lines []string
}

func (c *collectLog) add(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
}

func (c *collectLog) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestWatchdogDumpsOnArtificialHang is the acceptance check for #2163: a pass
// that overstays the threshold produces exactly one goroutine dump in the
// state dir, and the recovery is logged once the pass ends.
func TestWatchdogDumpsOnArtificialHang(t *testing.T) {
	dir := t.TempDir()
	logs := &collectLog{}
	ConfigureWatchdog(1, func() string { return dir }, logs.add)
	// Drop the threshold under the test's patience; ConfigureWatchdog clamps
	// at whole seconds for config, so set the atomic directly.
	wd.thresholdNanos.Store(int64(100 * time.Millisecond))

	LoopEnter("artificial-hang")
	waitFor(t, "stall dump", func() bool {
		files, _ := os.ReadDir(dir)
		return len(files) == 1
	})
	// One dump per episode: staying stalled must not write a second file.
	time.Sleep(300 * time.Millisecond)
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("want exactly 1 dump file, got %d (err %v)", len(files), err)
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	dump := string(data)
	if !strings.Contains(dump, "artificial-hang") {
		t.Errorf("dump header does not name the stalled pass:\n%.200s", dump)
	}
	if !strings.Contains(dump, "goroutine ") {
		t.Errorf("dump carries no goroutine stacks:\n%.200s", dump)
	}
	if !strings.HasPrefix(files[0].Name(), "ike-watchdog-") {
		t.Errorf("unexpected dump file name %q", files[0].Name())
	}

	LoopExit()
	waitFor(t, "recovery log line", func() bool {
		for _, l := range logs.all() {
			if strings.Contains(l, "recovered") {
				return true
			}
		}
		return false
	})
	for _, l := range logs.all() {
		if strings.Contains(l, "stalled") && !strings.Contains(l, dir) {
			t.Errorf("stall log does not name the dump path: %q", l)
		}
	}
	ConfigureWatchdog(0, nil, nil)
}

// TestWatchdogQuietUnderThreshold: ordinary fast passes never dump.
func TestWatchdogQuietUnderThreshold(t *testing.T) {
	dir := t.TempDir()
	logs := &collectLog{}
	ConfigureWatchdog(1, func() string { return dir }, logs.add)
	wd.thresholdNanos.Store(int64(150 * time.Millisecond))
	for i := 0; i < 20; i++ {
		LoopEnter("fast-pass")
		time.Sleep(time.Millisecond)
		LoopExit()
	}
	time.Sleep(300 * time.Millisecond)
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Errorf("no dump expected for fast passes, got %d files", len(files))
	}
	ConfigureWatchdog(0, nil, nil)
}

// TestWatchdogDisabledByZero: threshold 0 never dumps, however long a pass
// stays in flight.
func TestWatchdogDisabledByZero(t *testing.T) {
	dir := t.TempDir()
	ConfigureWatchdog(0, func() string { return dir }, nil)
	LoopEnter("disabled-hang")
	time.Sleep(200 * time.Millisecond)
	LoopExit()
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Errorf("disabled watchdog wrote %d dump files", len(files))
	}
}

// TestLoopPassesCounts: the pass counter advances on completed outermost
// passes only — the seam app wiring tests assert against.
func TestLoopPassesCounts(t *testing.T) {
	before := LoopPasses()
	LoopEnter("outer")
	LoopEnter("nested")
	LoopExit()
	if got := LoopPasses(); got != before {
		t.Fatalf("nested exit must not count a pass: %d -> %d", before, got)
	}
	LoopExit()
	if got := LoopPasses(); got != before+1 {
		t.Fatalf("want %d passes, got %d", before+1, got)
	}
}

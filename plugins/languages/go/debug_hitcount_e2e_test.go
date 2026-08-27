package langgo

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"ike/internal/dap"
)

// TestDelveHitCountBreakpoint drives a real `dlv dap` against a hit-count
// breakpoint (#2245): a breakpoint inside a five-pass loop that only arms
// from the third hit stops later than the first pass and fewer times than an
// unconditional one. The exact count depends on how the adapter reads the
// condition (delve accepts the VS Code forms), so the assertions pin what
// every reading agrees on: the first stop is the third pass (i == 2), and
// the loop is not stopped on every pass. Self-skips like TestDelveEndToEnd.
func TestDelveHitCountBreakpoint(t *testing.T) {
	if _, err := dlvResolve("dlv"); err != nil {
		t.Skip("dlv not installed")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("DevToolsSecurity", "-status").CombinedOutput()
		if err == nil && strings.Contains(string(out), "disabled") {
			t.Skip("macOS developer mode disabled — debugserver cannot attach headlessly (sudo DevToolsSecurity -enable)")
		}
	}

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mainGo := filepath.Join(dir, "main.go")
	src := `package main

import "fmt"

func main() {
	total := 0
	for i := 0; i < 5; i++ {
		total += i
	}
	fmt.Println("done", total)
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module e2ehit\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainGo, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	const bpLine = 7 // 0-based: `total += i`

	rwc, connErr := (toolchain{}).DebugAdapterConnect(dir, "")
	if connErr != nil {
		t.Fatal(connErr)
	}

	var mu sync.Mutex
	var initialized, stopped, terminated int
	var launchErr error
	onEvent := func(ev dap.Event) {
		mu.Lock()
		defer mu.Unlock()
		switch ev.Name {
		case "initialized":
			initialized++
		case "stopped":
			stopped++
		case "terminated":
			terminated++
		}
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			ok, err := cond(), launchErr
			mu.Unlock()
			if err != nil {
				t.Fatalf("launch failed: %v", err)
			}
			if ok {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	s := dap.Connect(rwc, onEvent)
	defer s.Close()
	if err := s.Initialize(); err != nil {
		t.Fatal(err)
	}
	if !s.SupportsHitConditionalBreakpoints() {
		t.Fatal("delve did not advertise hit-count support")
	}

	launched := s.LaunchAsync(map[string]any{
		"request": "launch", "mode": "debug", "program": mainGo, "cwd": dir,
	})
	go func() {
		if err := <-launched; err != nil {
			mu.Lock()
			launchErr = err
			mu.Unlock()
		}
	}()
	waitFor("initialized event", func() bool { return initialized >= 1 })
	bps, err := s.SetBreakpoints(mainGo, []dap.SourceBreakpoint{{Line: bpLine, HitCondition: "== 3"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bps) != 1 || !bps[0].Verified {
		t.Fatalf("breakpoint not verified: %+v", bps)
	}
	if err := s.ConfigurationDone(); err != nil {
		t.Fatal(err)
	}

	waitFor("hit-count stop", func() bool { return stopped >= 1 })
	threads, err := s.Threads()
	if err != nil || len(threads) == 0 {
		t.Fatalf("threads = %v (%v)", threads, err)
	}
	frames, err := s.StackTrace(threads[0].ID)
	if err != nil || len(frames) == 0 {
		t.Fatalf("frames = %v (%v)", frames, err)
	}
	// The third hit is the third loop pass: i == 2, not the first pass.
	if res, err := s.Evaluate("i", frames[0].ID, "watch"); err != nil || res.Result != "2" {
		t.Fatalf("evaluate i at the first stop = %+v (%v), want 2", res, err)
	}

	if err := s.Continue(threads[0].ID); err != nil {
		t.Fatal(err)
	}
	waitFor("termination", func() bool { return terminated >= 1 })
	mu.Lock()
	defer mu.Unlock()
	if stopped >= 5 {
		t.Fatalf("stopped %d times — the hit count did not gate the breakpoint", stopped)
	}
}

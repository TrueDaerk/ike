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

// TestDelveConnectHandshake exercises the real spawn-banner-dial seam
// (#1914) without launching a debuggee: `dlv dap` starts, announces its
// port, and answers initialize with the capabilities IKE gates on. Works
// even where debugserver may not attach (macOS developer mode off);
// self-skips only without dlv.
func TestDelveConnectHandshake(t *testing.T) {
	if _, err := dlvResolve("dlv"); err != nil {
		t.Skip("dlv not installed")
	}
	rwc, err := (toolchain{}).DebugAdapterConnect(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	s := dap.Connect(rwc, nil)
	defer s.Close()
	if err := s.Initialize(); err != nil {
		t.Fatal(err)
	}
	if !s.SupportsConditionalBreakpoints() || !s.SupportsHitConditionalBreakpoints() || !s.SupportsLogPoints() {
		t.Fatal("delve must advertise conditional-breakpoint, hit-count and logpoint support")
	}
}

// TestDelveEndToEnd drives a real `dlv dap` against a real Go program
// (#1914): the conditional breakpoint stops only when its condition is true,
// the logpoint logs without stopping, evaluate answers watch expressions,
// and stepping works. Self-skips when dlv or go are absent — and on macOS
// without developer mode, where debugserver blocks forever waiting for an
// attach authorization no headless run can grant — like the PHP bridge's
// e2e_real_test.go.
func TestDelveEndToEnd(t *testing.T) {
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

	// Resolve the symlinked macOS temp dir (/var → /private/var): delve
	// matches breakpoint paths against the compiler's recorded ones.
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
		fmt.Println(total)
	}
	fmt.Println("done", total)
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module e2e\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainGo, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	const (
		bpLine  = 7 // 0-based: `total += i`
		logLine = 8 // 0-based: `fmt.Println(total)`
	)

	rwc, connErr := (toolchain{}).DebugAdapterConnect(dir, "")
	if connErr != nil {
		t.Fatal(connErr)
	}

	var mu sync.Mutex
	var initialized, stopped, terminated int
	var outputs, seen []string
	var launchErr error
	onEvent := func(ev dap.Event) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, ev.Name)
		switch ev.Name {
		case "initialized":
			initialized++
		case "stopped":
			stopped++
		case "terminated":
			terminated++
		case "output":
			outputs = append(outputs, ev.Output().Output)
		}
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			ok := cond()
			err := launchErr
			mu.Unlock()
			if err != nil {
				t.Fatalf("launch failed: %v", err)
			}
			if ok {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("timed out waiting for %s; events seen: %v, outputs: %q", what, seen, outputs)
	}

	s := dap.Connect(rwc, onEvent)
	defer s.Close()
	if err := s.Initialize(); err != nil {
		t.Fatal(err)
	}
	if !s.SupportsConditionalBreakpoints() || !s.SupportsLogPoints() {
		t.Fatal("delve did not advertise conditional-breakpoint/logpoint support")
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
	// The DAP contract: configuration requests only after the initialized
	// event (delve emits it once the build finished), like the app does.
	waitFor("initialized event", func() bool { return initialized >= 1 })
	bps, err := s.SetBreakpoints(mainGo, []dap.SourceBreakpoint{
		{Line: bpLine, Condition: "i == 3"},
		{Line: logLine, LogMessage: "watchlog total={total}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bps) != 2 || !bps[0].Verified || !bps[1].Verified {
		t.Fatalf("breakpoints not verified: %+v", bps)
	}
	if err := s.ConfigurationDone(); err != nil {
		t.Fatal(err)
	}

	// The conditional breakpoint must stop on i == 3, not on the first pass.
	waitFor("conditional stop", func() bool { return stopped >= 1 })
	threads, err := s.Threads()
	if err != nil || len(threads) == 0 {
		t.Fatalf("threads = %v (%v)", threads, err)
	}
	frames, err := s.StackTrace(threads[0].ID)
	if err != nil || len(frames) == 0 {
		t.Fatalf("frames = %v (%v)", frames, err)
	}
	if frames[0].Line != bpLine+1 {
		t.Fatalf("stopped at line %d, want %d", frames[0].Line, bpLine+1)
	}
	if res, err := s.Evaluate("i", frames[0].ID, "watch"); err != nil || res.Result != "3" {
		t.Fatalf("evaluate i = %+v (%v), want 3", res, err)
	}
	// i==3 pauses before total += i, so total still holds 0+1+2.
	if res, err := s.Evaluate("total", frames[0].ID, "watch"); err != nil || res.Result != "3" {
		t.Fatalf("evaluate total = %+v (%v), want 3", res, err)
	}

	// Step over executes `total += i`.
	if err := s.Next(threads[0].ID); err != nil {
		t.Fatal(err)
	}
	waitFor("step stop", func() bool { return stopped >= 2 })
	frames, err = s.StackTrace(threads[0].ID)
	if err != nil || len(frames) == 0 {
		t.Fatalf("frames after step = %v (%v)", frames, err)
	}
	if res, err := s.Evaluate("total", frames[0].ID, "watch"); err != nil || res.Result != "6" {
		t.Fatalf("evaluate total after step = %+v (%v), want 6", res, err)
	}

	if err := s.Continue(threads[0].ID); err != nil {
		t.Fatal(err)
	}
	waitFor("termination", func() bool { return terminated >= 1 })

	// The logpoint logged on every loop pass without ever stopping there:
	// exactly one stop from the condition (plus the step) happened, and the
	// interpolated messages arrived as output events.
	mu.Lock()
	defer mu.Unlock()
	if stopped != 2 {
		t.Fatalf("stopped %d times, want 2 (condition + step) — the logpoint must not stop", stopped)
	}
	logged := 0
	for _, o := range outputs {
		if strings.Contains(o, "watchlog total=") {
			logged++
		}
	}
	if logged < 5 {
		t.Fatalf("logpoint logged %d times, want 5; outputs: %q", logged, outputs)
	}
}

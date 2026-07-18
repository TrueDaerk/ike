package bridge

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/dap"
)

// TestRealXdebugEndToEnd drives the bridge against a real php+Xdebug when
// one is available (skipped otherwise): breakpoint hit, stack, variables,
// setVariable, step, run to completion.
func TestRealXdebugEndToEnd(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php not on PATH")
	}
	if out, err := exec.Command(php, "-m").Output(); err != nil || !containsFold(string(out), "xdebug") {
		t.Skip("Xdebug not loaded")
	}

	// EvalSymlinks: macOS TempDir lives under /var → /private/var; Xdebug
	// reports the resolved path, and the test compares paths verbatim.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "main.php")
	src := `<?php
$greeting = "hello";
$numbers = [1, 2, 3];
$count = count($numbers);
echo $greeting, " ", $count, "\n";
`
	if err := os.WriteFile(script, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	rwc := New(php)
	events := make(chan dap.Event, 64)
	s := dap.Connect(rwc, func(ev dap.Event) { events <- ev })
	t.Cleanup(s.Close)
	s.OnRunInTerminal(func(seq int, args dap.RunInTerminalArgs) {
		cmd := exec.Command(args.Args[0], args.Args[1:]...)
		cmd.Dir = args.Cwd
		if err := cmd.Start(); err != nil {
			go func() { _ = s.RefuseReverse(seq, "runInTerminal", err.Error()) }()
			return
		}
		go func() { _ = cmd.Wait() }()
		go func() { _ = s.RespondRunInTerminal(seq, cmd.Process.Pid) }()
	})

	if err := s.Initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	launchDone := s.LaunchAsync(map[string]any{"request": "launch", "program": script, "cwd": dir})
	if err := <-launchDone; err != nil {
		t.Fatalf("launch: %v", err)
	}
	waitRealEvent(t, events, "initialized")

	// Break on `$count = count($numbers);` (0-based line 3 → wire line 4).
	bps, err := s.SetBreakpoints(script, []int{3})
	if err != nil || len(bps) != 1 || !bps[0].Verified {
		t.Fatalf("setBreakpoints: %v %+v", err, bps)
	}
	if err := s.ConfigurationDone(); err != nil {
		t.Fatalf("configurationDone: %v", err)
	}
	waitRealEvent(t, events, "stopped")

	frames, err := s.StackTrace(1)
	if err != nil || len(frames) == 0 {
		t.Fatalf("stackTrace: %v %+v", err, frames)
	}
	if frames[0].Line != 4 || frames[0].Source.Path != script {
		t.Fatalf("unexpected top frame: %+v", frames[0])
	}

	scopes, err := s.Scopes(frames[0].ID)
	if err != nil || len(scopes) == 0 {
		t.Fatalf("scopes: %v %+v", err, scopes)
	}
	vars, err := s.Variables(scopes[0].VariablesReference)
	if err != nil {
		t.Fatalf("variables: %v", err)
	}
	byName := map[string]dap.Variable{}
	for _, v := range vars {
		byName[v.Name] = v
	}
	if byName["$greeting"].Value != `"hello"` {
		t.Errorf("$greeting = %+v", byName["$greeting"])
	}
	arr := byName["$numbers"]
	if arr.Value != "array(3)" || arr.VariablesReference == 0 {
		t.Fatalf("$numbers = %+v", arr)
	}
	kids, err := s.Variables(arr.VariablesReference)
	if err != nil || len(kids) != 3 || kids[2].Value != "3" {
		t.Fatalf("array children: %v %+v", err, kids)
	}

	// Change $greeting, step over the count() line, verify the new value.
	if !s.SupportsSetVariable() {
		t.Fatal("supportsSetVariable not advertised")
	}
	if _, err := s.SetVariable(scopes[0].VariablesReference, "$greeting", "\"bye\""); err != nil {
		t.Fatalf("setVariable: %v", err)
	}
	if err := s.Next(1); err != nil {
		t.Fatalf("next: %v", err)
	}
	waitRealEvent(t, events, "stopped")
	frames, err = s.StackTrace(1)
	if err != nil || len(frames) == 0 || frames[0].Line != 5 {
		t.Fatalf("post-step frame: %v %+v", err, frames)
	}
	scopes, err = s.Scopes(frames[0].ID)
	if err != nil {
		t.Fatalf("post-step scopes: %v", err)
	}
	vars, err = s.Variables(scopes[0].VariablesReference)
	if err != nil {
		t.Fatalf("post-step variables: %v", err)
	}
	for _, v := range vars {
		if v.Name == "$count" && v.Value != "3" {
			t.Errorf("$count = %+v", v)
		}
		if v.Name == "$greeting" && v.Value != `"bye"` {
			t.Errorf("$greeting after set = %+v", v)
		}
	}

	if err := s.Continue(1); err != nil {
		t.Fatalf("continue: %v", err)
	}
	waitRealEvent(t, events, "terminated")
}

func containsFold(haystack, needle string) bool {
	h, n := []byte(haystack), []byte(needle)
	for i := 0; i+len(n) <= len(h); i++ {
		ok := true
		for j := range n {
			c, d := h[i+j], n[j]
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if 'A' <= d && d <= 'Z' {
				d += 'a' - 'A'
			}
			if c != d {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func waitRealEvent(t *testing.T, events chan dap.Event, name string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Name == name {
				return
			}
		case <-deadline:
			t.Fatalf("event %q did not arrive", name)
		}
	}
}

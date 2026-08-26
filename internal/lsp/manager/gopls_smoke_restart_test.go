//go:build goplssmoke

// End-to-end crash recovery against a real gopls (#2148), gated behind the
// `goplssmoke` build tag like the other smoke tests:
//
//	go test -tags goplssmoke ./internal/lsp/manager/ -run SmokeGoplsRestart -v
//
// It kills the spawned server the way an external `kill -9` would and expects
// diagnostics to come back on the same, never-reopened document.
package manager

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestSmokeGoplsRestartsAfterExternalKill (#2148): killing the language server
// process from outside must be invisible to the user — the manager respawns it
// after the backoff, re-sends the open document and diagnostics resume without
// the file being reopened.
func TestSmokeGoplsRestartsAfterExternalKill(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH")
	}
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	diagCh := make(chan []protocol.Diagnostic, 8)
	statusCh := make(chan string, 32)
	spec := lsp.ServerSpec{Language: "go", Command: "gopls", RootMarkers: []string{"go.mod"}}
	m := New(func(string) (lsp.ServerSpec, bool) { return spec, true }, nil, Callbacks{
		Diagnostics: func(path string, p protocol.PublishDiagnosticsParams, lines []string, enc string) {
			diagCh <- p.Diagnostics
		},
		Status: func(lang, text string, kind lsp.ServerStatusKind) {
			t.Logf("status: %s", text)
			select {
			case statusCh <- text:
			default:
			}
		},
	})
	defer m.Shutdown()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module smoke\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.go")
	src := "package main\n\nfunc main() {\n\tx := undefinedThing\n\t_ = x\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(path, "go", src); err != nil {
		t.Fatalf("open: %v", err)
	}
	if ds := waitDiagnostics(t, diagCh, 30*time.Second); len(ds) == 0 {
		t.Fatal("expected diagnostics for the undefined symbol before the kill")
	}

	killed := killChildProcesses(t, "gopls")
	if killed == 0 {
		t.Skip("no gopls child process found to kill")
	}

	// The first backoff step is one second; recovery must not need any user
	// interaction — no reopen, no edit.
	waitStatus(t, statusCh, "restarted", 60*time.Second)
	if ds := waitDiagnostics(t, diagCh, 60*time.Second); len(ds) == 0 {
		t.Fatal("no diagnostics after the automatic restart")
	}
}

// waitDiagnostics returns the first non-empty publish within d.
func waitDiagnostics(t *testing.T, ch chan []protocol.Diagnostic, d time.Duration) []protocol.Diagnostic {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ds := <-ch:
			if len(ds) > 0 {
				return ds
			}
		case <-deadline:
			t.Fatal("timed out waiting for diagnostics")
		}
	}
}

// waitStatus waits for a status message containing want.
func waitStatus(t *testing.T, ch chan string, want string, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case s := <-ch:
			if strings.Contains(s, want) {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %q status", want)
		}
	}
}

// killChildProcesses SIGKILLs this test process's own children whose command
// name contains name, and returns how many it hit. Scoped to our children on
// purpose: a bare `pkill gopls` would take down the developer's editor too.
func killChildProcesses(t *testing.T, name string) int {
	t.Helper()
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Logf("pgrep: %v", err)
		return 0
	}
	killed := 0
	for _, line := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		comm, err := exec.Command("ps", "-o", "comm=", "-p", line).Output()
		if err != nil || !strings.Contains(string(comm), name) {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			t.Logf("kill %d: %v", pid, err)
			continue
		}
		t.Logf("killed %s (pid %d)", strings.TrimSpace(string(comm)), pid)
		killed++
	}
	return killed
}

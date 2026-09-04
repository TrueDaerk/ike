//go:build goplssmoke

package manager

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestSmokeGoplsWarmResumeReusesServer is the #2492 instrumentation, kept as a
// smoke test: a project switch back to a parked workspace re-fires the
// EventFileOpened hook, which re-Opens every document — this measures what
// that costs against a real gopls. The warm re-open must reuse the live
// server (exactly one "starting" state over the whole run) and republish in
// well under a second; only a CloseRoot (the #1521 idle shutdown) forces the
// cold spawn + initialize + first-index price again.
func TestSmokeGoplsWarmResumeReusesServer(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH")
	}
	var mu sync.Mutex
	starting := 0
	diagCh := make(chan struct{}, 16)
	spec := lsp.ServerSpec{Language: "go", Command: "gopls", RootMarkers: []string{"go.mod"}}
	m := New(func(string) (lsp.ServerSpec, bool) { return spec, true }, nil, Callbacks{
		Diagnostics: func(path string, p protocol.PublishDiagnosticsParams, lines []string, enc string) {
			select {
			case diagCh <- struct{}{}:
			default:
			}
		},
		Status: func(lang, text string, kind lsp.ServerStatusKind) {
			if kind == lsp.ServerState && strings.Contains(text, "starting") {
				mu.Lock()
				starting++
				mu.Unlock()
			}
			t.Logf("status: %s", text)
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

	waitPublish := func(what string, limit time.Duration) time.Duration {
		t.Helper()
		begin := time.Now()
		select {
		case <-diagCh:
			d := time.Since(begin)
			t.Logf("%s: first publish after %v", what, d)
			return d
		case <-time.After(limit):
			t.Fatalf("%s: no publish within %v", what, limit)
			return 0
		}
	}

	// Cold start: spawn + initialize + first typecheck.
	if err := m.Open(path, "go", src); err != nil {
		t.Fatalf("cold open: %v", err)
	}
	waitPublish("cold start", 30*time.Second)

	// Warm resume: what performSwitch's fresh Init does for a parked root
	// whose server is still alive — a plain re-Open of the same document.
	for len(diagCh) > 0 {
		<-diagCh
	}
	if err := m.Open(path, "go", src); err != nil {
		t.Fatalf("warm re-open: %v", err)
	}
	if d := waitPublish("warm resume", 10*time.Second); d > time.Second {
		t.Errorf("warm resume took %v, want < 1s — the server was not reused?", d)
	}
	mu.Lock()
	if starting != 1 {
		t.Errorf("starting states = %d, want 1 — the warm re-open must not respawn", starting)
	}
	mu.Unlock()

	// Idle shutdown then resume: the honest cold price, paid again.
	m.CloseRoot(dir)
	for len(diagCh) > 0 {
		<-diagCh
	}
	if err := m.Open(path, "go", src); err != nil {
		t.Fatalf("post-idle open: %v", err)
	}
	waitPublish("resume after idle shutdown", 30*time.Second)
	mu.Lock()
	if starting != 2 {
		t.Errorf("starting states = %d, want 2 — the post-idle open must respawn", starting)
	}
	mu.Unlock()
}

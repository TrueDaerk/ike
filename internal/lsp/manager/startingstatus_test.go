package manager

import (
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/lsp"
)

// TestColdStartEmitsStartingStatus (#2492): a cold start is visible while it
// runs — the first Open of a (lang, root) reports "starting" as a state
// before "ready", so the status line's server segment (#380) is not blank
// through the spawn + initialize window. A second Open of the same root
// reuses the live server and repeats neither.
func TestColdStartEmitsStartingStatus(t *testing.T) {
	fakeLookPath(t, "shellcheck")
	rec := &statusRecorder{}
	m := New(resolver(shellSpec()), fakeConnector(), Callbacks{Status: rec.record})
	defer m.Shutdown()

	dir := t.TempDir()
	if err := m.Open(filepath.Join(dir, "a.sh"), "shell", "echo hi"); err != nil {
		t.Fatal(err)
	}

	starting := func() int {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		n := 0
		for i, txt := range rec.texts {
			if rec.kinds[i] == lsp.ServerState && strings.Contains(txt, "starting") {
				n++
			}
		}
		return n
	}
	ready := func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for i, txt := range rec.texts {
			if rec.kinds[i] == lsp.ServerState && strings.Contains(txt, "ready") {
				return true
			}
		}
		return false
	}
	waitFor(t, ready)
	if n := starting(); n != 1 {
		t.Fatalf("want exactly one starting state before ready, got %d", n)
	}

	// The reuse path stays silent: no second spawn, no second starting.
	if err := m.Open(filepath.Join(dir, "b.sh"), "shell", "echo ho"); err != nil {
		t.Fatal(err)
	}
	if n := starting(); n != 1 {
		t.Fatalf("reusing the live server must not report starting again, got %d", n)
	}
}

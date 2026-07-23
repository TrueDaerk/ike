package manager

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ike/internal/lsp"
)

// statusRecorder collects Status callbacks thread-safely.
type statusRecorder struct {
	mu    sync.Mutex
	texts []string
	kinds []lsp.ServerStatusKind
}

func (r *statusRecorder) record(lang, text string, kind lsp.ServerStatusKind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, text)
	r.kinds = append(r.kinds, kind)
}

// hints returns the recorded companion hints (warn events mentioning "not found").
func (r *statusRecorder) hints() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for i, t := range r.texts {
		if r.kinds[i] == lsp.ServerEventWarn && strings.Contains(t, "not found") {
			out = append(out, t)
		}
	}
	return out
}

// fakeLookPath makes only the listed binaries resolvable.
func fakeLookPath(t *testing.T, present ...string) {
	t.Helper()
	orig := lookPath
	lookPath = func(bin string) (string, error) {
		for _, p := range present {
			if p == bin {
				return "/fake/bin/" + bin, nil
			}
		}
		return "", errors.New("exec: " + bin + ": executable file not found in $PATH")
	}
	t.Cleanup(func() { lookPath = orig })
}

func shellSpec() lsp.ServerSpec {
	return lsp.ServerSpec{
		Language: "shell", Command: "fake", RootMarkers: []string{".git"},
		Companions: []lsp.Companion{
			{Binary: "shellcheck", Purpose: "shell diagnostics", Install: "brew install shellcheck"},
		},
	}
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}

// TestCompanionHintMissing guards #1067: a server becoming ready with a missing
// companion binary raises exactly one warn hint naming tool, purpose and recipe.
func TestCompanionHintMissing(t *testing.T) {
	fakeLookPath(t) // nothing on PATH
	rec := &statusRecorder{}
	m := New(resolver(shellSpec()), fakeConnector(), Callbacks{Status: rec.record})
	defer m.Shutdown()

	dir := t.TempDir()
	if err := m.Open(filepath.Join(dir, "a.sh"), "shell", "echo hi"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(rec.hints()) == 1 })
	want := "shellcheck not found — shell diagnostics disabled (brew install shellcheck)"
	if got := rec.hints()[0]; got != want {
		t.Errorf("hint = %q, want %q", got, want)
	}
}

// TestCompanionHintOnce guards the dedupe: more files and even a second root of
// the same language never repeat the hint within one manager lifetime.
func TestCompanionHintOnce(t *testing.T) {
	fakeLookPath(t)
	rec := &statusRecorder{}
	m := New(resolver(shellSpec()), fakeConnector(), Callbacks{Status: rec.record})
	defer m.Shutdown()

	dirA, dirB := t.TempDir(), t.TempDir()
	// Distinct roots so the second Open spawns a fresh server (full ready path).
	for _, d := range []string{dirA, dirB} {
		_ = os.Mkdir(filepath.Join(d, ".git"), 0o755)
	}
	if err := m.Open(filepath.Join(dirA, "a.sh"), "shell", "echo a"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(rec.hints()) == 1 })
	if err := m.Open(filepath.Join(dirA, "b.sh"), "shell", "echo b"); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(filepath.Join(dirB, "c.sh"), "shell", "echo c"); err != nil {
		t.Fatal(err)
	}
	// Give any spurious duplicate a moment to surface, then assert.
	time.Sleep(100 * time.Millisecond)
	if got := rec.hints(); len(got) != 1 {
		t.Errorf("hints = %v, want exactly one", got)
	}
}

// TestCompanionPresentNoHint: a companion on PATH produces no hint at all.
func TestCompanionPresentNoHint(t *testing.T) {
	fakeLookPath(t, "shellcheck")
	rec := &statusRecorder{}
	m := New(resolver(shellSpec()), fakeConnector(), Callbacks{Status: rec.record})
	defer m.Shutdown()

	dir := t.TempDir()
	if err := m.Open(filepath.Join(dir, "a.sh"), "shell", "echo hi"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for _, tx := range rec.texts {
			if strings.Contains(tx, "ready") {
				return true
			}
		}
		return false
	})
	if got := rec.hints(); len(got) != 0 {
		t.Errorf("hints = %v, want none", got)
	}
}

// TestCompanionHintPerMissingTool: each missing companion of a language gets its
// own hint, and one already-present tool is skipped.
func TestCompanionHintPerMissingTool(t *testing.T) {
	fakeLookPath(t, "ansible")
	spec := lsp.ServerSpec{
		Language: "ansible", Command: "fake",
		Companions: []lsp.Companion{
			{Binary: "ansible", Purpose: "ansible module docs and validation", Install: "pipx install ansible"},
			{Binary: "ansible-lint", Purpose: "ansible lint diagnostics", Install: "pipx install ansible-lint"},
		},
	}
	rec := &statusRecorder{}
	m := New(resolver(spec), fakeConnector(), Callbacks{Status: rec.record})
	defer m.Shutdown()

	dir := t.TempDir()
	if err := m.Open(filepath.Join(dir, "site.yml"), "ansible", "---"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(rec.hints()) == 1 })
	want := "ansible-lint not found — ansible lint diagnostics disabled (pipx install ansible-lint)"
	if got := rec.hints()[0]; got != want {
		t.Errorf("hint = %q, want %q", got, want)
	}
}

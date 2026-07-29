package terminal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// TestProcessCwdSelf checks the kernel query against the test process itself.
func TestProcessCwdSelf(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("processCwd unsupported on " + runtime.GOOS)
	}
	got := processCwd(os.Getpid())
	if got == "" {
		t.Fatal("processCwd returned empty for own pid")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(wd)
	if res, _ := filepath.EvalSymlinks(got); res != want {
		t.Fatalf("processCwd = %q, want %q", got, want)
	}
}

// TestProcessCwdDeadPid checks the failure path returns "" (fallback), not
// garbage.
func TestProcessCwdDeadPid(t *testing.T) {
	if got := processCwd(-1); got != "" {
		t.Fatalf("processCwd(-1) = %q, want empty", got)
	}
}

// TestSessionCwdFollowsCd is the #1383 regression: a plain shell without
// OSC 7 prompt integration `cd`s into a subdirectory and Cwd() must follow
// via the kernel query instead of sticking to the start directory.
func TestSessionCwdFollowsCd(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("kernel cwd query unsupported on " + runtime.GOOS)
	}
	c := &collector{}
	s := startSh(t, c)
	start, _ := filepath.EvalSymlinks(s.Dir())
	sub := filepath.Join(s.Dir(), "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "cwd reports start dir", func() bool {
		got, _ := filepath.EvalSymlinks(s.Cwd())
		return got == start
	})
	for _, r := range "cd sub" {
		s.SendKey(keyFor(r))
	}
	s.SendKey(vt.KeyPressEvent{Code: vt.KeyEnter})
	wantSub, _ := filepath.EvalSymlinks(sub)
	waitFor(t, "cwd follows cd", func() bool {
		got, _ := filepath.EvalSymlinks(s.Cwd())
		return got == wantSub
	})
}

// TestSessionCwdOSC7Wins: an OSC 7 report takes precedence over the kernel
// query — prompt-integration shells keep their documented behavior.
func TestSessionCwdOSC7Wins(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	s.mu.Lock()
	s.cwd = "/reported/by/osc7"
	s.mu.Unlock()
	if got := s.Cwd(); got != "/reported/by/osc7" {
		t.Fatalf("Cwd = %q, want OSC 7 report to win", got)
	}
}

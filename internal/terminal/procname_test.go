package terminal

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestProcessNameOwnProcess: the lookup names this very process; the exact
// binary name is the test binary's, so only "not empty" is asserted.
func TestProcessNameOwnProcess(t *testing.T) {
	if got := processName(os.Getpid()); got == "" {
		t.Fatal("processName returned empty for own pid")
	}
	if got := processName(0); got != "" {
		t.Errorf("processName(0) = %q, want empty", got)
	}
	if got := processName(-1); got != "" {
		t.Errorf("processName(-1) = %q, want empty", got)
	}
}

// TestCleanProcessName: ps reports a full path on macOS, procfs a trailing
// newline, and a login shell hides behind a leading dash.
func TestCleanProcessName(t *testing.T) {
	cases := map[string]string{
		"vim\n":                  "vim",
		"/usr/bin/vim":           "vim",
		"  npm  ":                "npm",
		"-zsh":                   "zsh",
		"/bin/zsh\nsomething":    "zsh",
		"":                       "",
		"   ":                    "",
		"/opt/homebrew/bin/node": "node",
	}
	for in, want := range cases {
		if got := cleanProcessName(in); got != want {
			t.Errorf("cleanProcessName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestForegroundNameIdleAndBusy: an idle shell owns its terminal itself, so
// there is no foreground process to name; a foreground job is named (#2702).
func TestForegroundNameIdleAndBusy(t *testing.T) {
	s := startSh(t, &collector{})
	waitFor(t, "idle prompt", func() bool { return !s.Busy() })
	if pid := s.ForegroundPid(); pid != 0 {
		t.Errorf("an idle shell has no foreground process, got pid %d", pid)
	}
	if name := s.ForegroundName(); name != "" {
		t.Errorf("an idle shell names nothing, got %q", name)
	}

	for _, r := range "sleep 30\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "sleep owns the foreground", func() bool { return s.Busy() })
	deadline := time.Now().Add(5 * time.Second)
	var name string
	for time.Now().Before(deadline) {
		if name = s.ForegroundName(); strings.Contains(name, "sleep") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the foreground job must be named, got %q", name)
}

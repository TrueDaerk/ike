package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// procname.go names the process a session currently runs in the foreground
// (#2702), so the close/quit guard can say *what* is running instead of
// "a running shell terminal". Best effort throughout: every lookup may fail
// (process gone, sandbox denial, unsupported platform) and then the guard
// falls back to its generic wording.

// processName returns the executable name of pid — the `comm` value, without
// a path — or "" when it cannot be determined. procfs is read directly where
// it exists; elsewhere `ps -o comm=` answers, which covers macOS and the
// BSDs. Called once per guard prompt, never on a render path.
func processName(pid int) string {
	if pid <= 0 {
		return ""
	}
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm"); err == nil {
		return cleanProcessName(string(data))
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return cleanProcessName(string(out))
}

// cleanProcessName trims a raw comm/ps line down to the bare binary name:
// `ps` reports the full path of the executable on macOS, and a login shell
// hides behind a leading dash ("-zsh").
func cleanProcessName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = filepath.Base(s)
	return strings.TrimPrefix(s, "-")
}

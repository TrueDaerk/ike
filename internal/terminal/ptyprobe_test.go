package terminal

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// ptyProbe is the once-per-binary answer to "can this environment run a
// shell on a pty and show a prompt?". Sandboxes and some CI runners cannot,
// and before #2997 every pty test found that out on its own by waiting the
// full waitFor deadline — dozens of tests, 5 s each. Now the first test pays
// the probe once and every later one skips within microseconds.
var (
	ptyProbeOnce sync.Once
	ptyProbeErr  error
)

// ptyProbeDeadline bounds the one probe. A working shell prints its prompt
// in well under a second; the margin covers a loaded CI box.
const ptyProbeDeadline = 5 * time.Second

// requirePTY skips t when the environment has no usable pty shell.
func requirePTY(t *testing.T) {
	t.Helper()
	ptyProbeOnce.Do(func() {
		if testShellErr != "" {
			ptyProbeErr = fmt.Errorf("%s (#3000)", testShellErr)
			return
		}
		c := &collector{}
		s, err := StartSession("probe", testShell, t.TempDir(), 80, 24, nil, c.send)
		if err != nil {
			ptyProbeErr = fmt.Errorf("spawn: %w", err)
			return
		}
		defer s.Close()
		deadline := time.Now().Add(ptyProbeDeadline)
		for !strings.Contains(plainView(s), "$") {
			if !time.Now().Before(deadline) {
				ptyProbeErr = fmt.Errorf("no shell prompt on the pty within %s", ptyProbeDeadline)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		// Second half: a command that prints and exits at once. The hosted
		// macOS CI VM delivers the interactive prompt but loses the output of
		// a fast-exiting child between the slave closing and the master read,
		// which is the failure mode of every command-session test (#2997).
		cs, err := StartCommandSession("probe", []string{"/bin/sh", "-c", "echo probe-ok"}, t.TempDir(), 80, 24, nil, c.send)
		if err != nil {
			ptyProbeErr = fmt.Errorf("command spawn: %w", err)
			return
		}
		defer cs.Close()
		for !strings.Contains(plainView(cs), "probe-ok") && !strings.Contains(ansi.Strip(cs.HistoryLine(0)), "probe-ok") {
			if !time.Now().Before(deadline) {
				ptyProbeErr = fmt.Errorf("a fast-exiting command's output never reached the grid within %s", ptyProbeDeadline)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	if ptyProbeErr != nil {
		t.Skipf("no usable pty shell in this environment: %v", ptyProbeErr)
	}
}

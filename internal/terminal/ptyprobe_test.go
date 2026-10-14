package terminal

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
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
		c := &collector{}
		s, err := StartSession("probe", "/bin/sh", t.TempDir(), 80, 24, nil, c.send)
		if err != nil {
			ptyProbeErr = fmt.Errorf("spawn: %w", err)
			return
		}
		defer s.Close()
		deadline := time.Now().Add(ptyProbeDeadline)
		for time.Now().Before(deadline) {
			if strings.Contains(plainView(s), "$") {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		ptyProbeErr = fmt.Errorf("no shell prompt on the pty within %s", ptyProbeDeadline)
	})
	if ptyProbeErr != nil {
		t.Skipf("no usable pty shell in this environment: %v", ptyProbeErr)
	}
}

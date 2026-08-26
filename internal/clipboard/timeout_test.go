package clipboard

import (
	"runtime"
	"testing"
	"time"
)

// TestCommandTimeoutKillsHungTool (#2163): clipboard subprocesses run on the
// bubbletea update loop, so a hung tool (osascript behind a TCC prompt, a
// wedged xclip) must be killed at the deadline instead of freezing the IDE.
func TestCommandTimeoutKillsHungTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no sleep binary")
	}
	old := cmdTimeout
	cmdTimeout = 100 * time.Millisecond
	defer func() { cmdTimeout = old }()

	cmd, cancel := command("sleep", "10")
	defer cancel()
	start := time.Now()
	err := cmd.Run()
	if err == nil {
		t.Fatal("a hung tool must return an error at the deadline")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("deadline did not kill the process (took %s)", elapsed)
	}
}

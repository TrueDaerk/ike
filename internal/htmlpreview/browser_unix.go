//go:build !windows

package htmlpreview

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup starts the browser as the leader of its own process group,
// and makes the context's kill (timeout, cancellation) take the whole group:
// Chrome's renderer, GPU and crash-handler helpers would otherwise outlive a
// killed parent.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// reapProcessGroup kills whatever is left of the browser's process group
// after it exited: helpers still running would keep writing into the render
// directory being removed.
func reapProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

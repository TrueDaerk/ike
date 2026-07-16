//go:build !windows

package transport

import (
	"os/exec"
	"syscall"
)

// detach places cmd in a new session (setsid), so it has no controlling
// terminal and cannot deliver job-control signals to — or steal the tty from —
// the TUI process. See Spec.Detached (#620).
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}

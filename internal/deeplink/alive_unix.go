//go:build !windows

package deeplink

import (
	"errors"
	"syscall"
)

// pidAlive reports whether a process with pid exists. Signal 0 checks
// without delivering anything; EPERM means it exists but belongs to someone
// else — alive all the same.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

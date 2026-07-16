//go:build windows

package transport

import "os/exec"

// detach is a no-op on Windows: there is no controlling-terminal/session model
// to escape, and DAP adapters communicate purely over stdio. See Spec.Detached.
func detach(_ *exec.Cmd) {}

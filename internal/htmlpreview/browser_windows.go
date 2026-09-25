//go:build windows

package htmlpreview

import "os/exec"

// ownProcessGroup is a no-op on Windows: exec.CommandContext kills the
// browser process itself.
func ownProcessGroup(*exec.Cmd) {}

// reapProcessGroup is a no-op on Windows.
func reapProcessGroup(*exec.Cmd) {}

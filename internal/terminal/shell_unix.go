//go:build !windows

package terminal

// platformShell is the unix resolution order ($SHELL, /bin/sh).
var platformShell = unixShell

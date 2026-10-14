//go:build !windows

package terminal

import (
	"os"

	"golang.org/x/sys/unix"
)

// foregroundPgrp returns the foreground process group of the terminal behind
// ptmx (TIOCGPGRP).
func foregroundPgrp(ptmx *os.File) (int, error) {
	return unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPGRP)
}

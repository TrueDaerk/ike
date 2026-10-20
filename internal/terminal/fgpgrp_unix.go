//go:build !windows

package terminal

import (
	"errors"

	"golang.org/x/sys/unix"
)

// foregroundPgrp returns the foreground process group of the terminal behind
// ptmx (TIOCGPGRP).
func foregroundPgrp(ptmx ptyMaster) (int, error) {
	p, ok := ptmx.(*unixPTY)
	if !ok {
		return 0, errors.ErrUnsupported
	}
	return unix.IoctlGetInt(int(p.Fd()), unix.TIOCGPGRP)
}

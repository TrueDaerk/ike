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
	// Through SyscallConn rather than Fd: the reader goroutine closes the pty
	// when the shell exits, and Control holds the descriptor against that
	// close for the duration of the ioctl instead of racing it.
	rc, err := p.SyscallConn()
	if err != nil {
		return 0, err
	}
	var (
		pgrp int
		ierr error
	)
	if err := rc.Control(func(fd uintptr) {
		pgrp, ierr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP)
	}); err != nil {
		return 0, err
	}
	return pgrp, ierr
}

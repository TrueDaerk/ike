//go:build !windows

package terminal

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// unixPTY is the master end of a creack/pty pair as the session's ptyMaster.
type unixPTY struct{ *os.File }

// Resize applies TIOCSWINSZ; the child gets SIGWINCH from the kernel.
func (p *unixPTY) Resize(w, h int) error {
	return pty.Setsize(p.File, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
}

// startOnPTY starts cmd as the session leader of a fresh pty sized w×h and
// returns both ends. The slave stays open in the parent on purpose (#2997):
// creack/pty's Start closes it right after the fork, and on macOS a child
// that prints and exits at once can then drain the master's queue into
// nothing — the reader wakes to EIO with the output gone. That showed as
// empty Run tools on a loaded machine and as every command-session test
// failing on the hosted CI VM. Keeping one slave fd until the child has
// exited keeps the queue alive; the reaper closes it after Wait, which is
// when the reader's EIO means "drained", not "lost".
func startOnPTY(cmd *exec.Cmd, w, h int) (ptmx ptyMaster, tty *os.File, err error) {
	master, tty, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	if err := pty.Setsize(master, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)}); err != nil {
		_ = master.Close()
		_ = tty.Close()
		return nil, nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	if err := cmd.Start(); err != nil {
		_ = master.Close()
		_ = tty.Close()
		return nil, nil, err
	}
	return &unixPTY{master}, tty, nil
}

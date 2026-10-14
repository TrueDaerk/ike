//go:build windows

package terminal

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// startOnPTY on Windows defers to creack/pty, which owns the console pair;
// there is no slave fd to hold (see spawn_unix.go for why unix keeps one).
func startOnPTY(cmd *exec.Cmd, w, h int) (ptmx, tty *os.File, err error) {
	ptmx, err = pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
	return ptmx, nil, err
}

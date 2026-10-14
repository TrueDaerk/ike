//go:build !windows

package diag

import (
	"os"
	"syscall"
)

// dumpSignals are the signals that trigger a Dump: SIGUSR1.
func dumpSignals() []os.Signal { return []os.Signal{syscall.SIGUSR1} }

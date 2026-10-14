//go:build windows

package diag

import "os"

// dumpSignals is empty on Windows, which has no SIGUSR1: the dump is reachable
// through the IKE_PPROF endpoint only (#3002).
func dumpSignals() []os.Signal { return nil }

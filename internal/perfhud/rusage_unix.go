//go:build !windows

package perfhud

import (
	"runtime"
	"syscall"
)

// peakRSS returns getrusage's ru_maxrss in bytes, or 0 when unavailable.
func peakRSS() uint64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	if ru.Maxrss <= 0 {
		return 0
	}
	n := uint64(ru.Maxrss)
	if runtime.GOOS != "darwin" {
		// Darwin reports bytes; Linux and the BSDs report kilobytes.
		n *= 1024
	}
	return n
}

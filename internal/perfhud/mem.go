package perfhud

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// mem.go resolves the process footprint the Go runtime does not report. The
// heap numbers come from runtime.ReadMemStats, but a "why is IKE at 400 MB"
// bug report needs the resident set the OS accounts for — the number the user
// sees in Activity Monitor or top, which includes the runtime's freed-but-not-
// returned pages (#1537).
//
// Linux exposes the current RSS in /proc/self/statm. macOS has no equivalent
// without cgo (task_info), so it falls back to getrusage's ru_maxrss, which is
// the process *peak* — honest as long as it is labelled as such, which is what
// the peak return value is for.

// rss returns the resident set size in bytes and whether the value is the
// process peak rather than the current footprint. A zero size means the
// platform gave no answer; consumers omit the field then.
func rss() (bytes uint64, peak bool) {
	if b, err := os.ReadFile("/proc/self/statm"); err == nil {
		// statm: size resident shared text lib data dt — all in pages.
		if fields := strings.Fields(string(b)); len(fields) >= 2 {
			if pages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				return pages * uint64(os.Getpagesize()), false
			}
		}
	}
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, false
	}
	if ru.Maxrss <= 0 {
		return 0, false
	}
	n := uint64(ru.Maxrss)
	if runtime.GOOS != "darwin" {
		// Darwin reports bytes; Linux and the BSDs report kilobytes.
		n *= 1024
	}
	return n, true
}

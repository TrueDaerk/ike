package diag

import (
	"fmt"
	"runtime"
)

// memstats.go summarizes the Go heap for the diag.memoryStats command (#1537).
// The point of the breakdown is telling a real leak from runtime-retained
// pages: HeapInuse is live data the program still references, while
// HeapIdle−HeapReleased is memory the runtime freed but has not returned to
// the OS — on macOS that portion stays counted against the process footprint
// until memory pressure, which can read as a "leak" that isn't one.

// MemSummary returns a one-line heap summary from runtime.ReadMemStats.
func MemSummary() string {
	var s runtime.MemStats
	runtime.ReadMemStats(&s)
	retained := s.HeapIdle - s.HeapReleased
	return fmt.Sprintf(
		"heap %s in use, %s freed-not-returned, %s released; %s from OS; %d goroutines, %d GCs",
		fmtBytes(s.HeapInuse), fmtBytes(retained), fmtBytes(s.HeapReleased),
		fmtBytes(s.Sys), runtime.NumGoroutine(), s.NumGC,
	)
}

// fmtBytes renders a byte count with one decimal in the fitting unit.
func fmtBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	}
}

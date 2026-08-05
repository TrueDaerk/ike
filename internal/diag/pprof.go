// Package diag ships the opt-in runtime diagnostics hooks (#1001): a pprof
// HTTP endpoint gated by IKE_PPROF and a SIGUSR1-triggered goroutine/heap
// dump. Both are off by default — a TUI has no place for a surprise listener.
package diag

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"syscall"
	"time"
)

// Start wires the opt-in diagnostics:
//
//   - IKE_PPROF=<addr> (e.g. "localhost:6060") serves net/http/pprof on addr —
//     `go tool pprof http://localhost:6060/debug/pprof/profile` etc.
//   - SIGUSR1 writes a goroutine dump plus a heap profile to the directory
//     named by IKE_PPROF_DIR (default: the OS temp dir), stamped with the pid.
//
// Failures are reported through warn (stderr in main) and never abort startup.
func Start(warn func(string)) {
	if addr := os.Getenv("IKE_PPROF"); addr != "" {
		go func() {
			// DefaultServeMux carries the /debug/pprof handlers via the
			// blank import above.
			if err := http.ListenAndServe(addr, nil); err != nil {
				warn("pprof listener: " + err.Error())
			}
		}()
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGUSR1)
	go func() {
		for range sig {
			if _, err := Dump(); err != nil {
				warn("pprof dump: " + err.Error())
			}
		}
	}()
}

// Dump writes a goroutine dump plus a heap profile to IKE_PPROF_DIR (default:
// the OS temp dir) and returns the common file-name prefix of the pair. It
// backs both the SIGUSR1 handler and the diag.heapDump palette command
// (#1537), so a bloated live session can be profiled without a restart.
func Dump() (prefix string, err error) {
	dir := os.Getenv("IKE_PPROF_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	stamp := fmt.Sprintf("ike-%d-%s", os.Getpid(), time.Now().Format("150405"))
	prefix = filepath.Join(dir, stamp)
	g, err := os.Create(prefix + "-goroutines.txt")
	if err != nil {
		return "", err
	}
	defer g.Close()
	if err := pprof.Lookup("goroutine").WriteTo(g, 1); err != nil {
		return "", err
	}
	fmt.Fprintf(g, "\ntotal goroutines: %d\n", runtime.NumGoroutine())
	h, err := os.Create(prefix + "-heap.pprof")
	if err != nil {
		return "", err
	}
	defer h.Close()
	runtime.GC()
	if err := pprof.Lookup("heap").WriteTo(h, 0); err != nil {
		return "", err
	}
	return prefix, nil
}

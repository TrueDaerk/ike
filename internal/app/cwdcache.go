package app

import (
	"os"
	"sync"
)

// cwdcache.go caches the process working directory for the render hot path
// (#608). Profiling a fullscreen scroll showed `os.Getwd()` — a `stat` syscall
// on macOS — accounting for ~49% of all CPU: it was called every frame from the
// terminal title (`displayDir`), the status line (`displayPath`), and the
// breakpoint gutter (`projectRoot`), each of them per pane. The working directory
// only changes on a project switch (`os.Chdir`), so it is read once and cached
// until explicitly invalidated there.

var (
	cwdMu    sync.RWMutex
	cwdValue string
	cwdKnown bool
)

// cachedGetwd returns the working directory, reading it from the OS only on the
// first call after startup or an invalidateCwd. It mirrors os.Getwd's contract
// (absolute path, error on failure) but costs a map-free lookup on the hot path.
func cachedGetwd() (string, error) {
	cwdMu.RLock()
	if cwdKnown {
		v := cwdValue
		cwdMu.RUnlock()
		return v, nil
	}
	cwdMu.RUnlock()

	cwdMu.Lock()
	defer cwdMu.Unlock()
	if cwdKnown { // filled while we waited for the write lock
		return cwdValue, nil
	}
	v, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cwdValue, cwdKnown = v, true
	return v, nil
}

// invalidateCwd drops the cached working directory so the next cachedGetwd reads
// it fresh. Call it right after an os.Chdir (project switch).
func invalidateCwd() {
	cwdMu.Lock()
	cwdKnown = false
	cwdMu.Unlock()
}

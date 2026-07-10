package wasm

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// scan.go is the startup discovery half of #23: every `.wasm` file in the
// plugins directory loads into the runtime; a faulting file is recorded as a
// diagnostic and skipped — one broken plugin never stops the scan or IKE.

// ScanResult reports one directory scan.
type ScanResult struct {
	Loaded []*Module
	// Diagnostics carries one human-readable line per file that failed to
	// load (the module stays unloaded).
	Diagnostics []string
}

// ScanDir loads every *.wasm in dir (non-recursive, deterministic name
// order). A missing directory is normal — plugins are optional — and yields
// an empty result.
func (r *Runtime) ScanDir(dir string) ScanResult {
	var res ScanResult
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			res.Diagnostics = append(res.Diagnostics, fmt.Sprintf("plugins dir %s: %v", dir, err))
		}
		return res
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".wasm") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		m, err := r.Load(filepath.Join(dir, name))
		if err != nil {
			res.Diagnostics = append(res.Diagnostics, err.Error())
			continue
		}
		res.Loaded = append(res.Loaded, m)
	}
	return res
}

// DefaultDir is the conventional plugins directory: $IKE_CONFIG_DIR/plugins
// when the override is set, ~/.ike/plugins otherwise (mirroring the config
// discovery seam).
func DefaultDir() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "plugins")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "plugins")
}

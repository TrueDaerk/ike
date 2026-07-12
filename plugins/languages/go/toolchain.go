package langgo

import (
	"os"
	"os/exec"
)

// toolchain resolves the go binary (#538): PATH first, then the common install
// locations — a GUI-launched process often misses /opt/homebrew/bin on PATH.
// gopls needs no interpreter injection, so Detect contributes nothing — the
// value feeds the settings page's display, the explicit [lang.go] interpreter
// choice, and the terminal PATH shims.
type toolchain struct{}

// goLook and goFallbacks are seams for tests.
var (
	goLook      = exec.LookPath
	goFallbacks = []string{"/opt/homebrew/bin/go", "/usr/local/bin/go", "/usr/local/go/bin/go", "/usr/bin/go"}
)

// Detect implements lang.Toolchain: nothing to inject into the server.
func (toolchain) Detect(string) (map[string]any, bool) { return nil, false }

// Interpreter implements lang.InterpreterDetector.
func (toolchain) Interpreter(string) (string, bool) {
	if p, err := goLook("go"); err == nil {
		return p, true
	}
	for _, p := range goFallbacks {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

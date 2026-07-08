package langphp

import (
	"os"
	"os/exec"
)

// toolchain resolves the PHP interpreter (Roadmap 0160): PATH first, then the
// common install locations. Intelephense needs no interpreter injection, so
// Detect contributes nothing — the value feeds the settings page's display,
// the explicit [lang.php] interpreter choice, and 0170's terminal PATH shims.
type toolchain struct{}

// Detect implements lang.Toolchain: nothing to inject into the server.
func (toolchain) Detect(string) (map[string]any, bool) { return nil, false }

// Interpreter implements lang.InterpreterDetector.
func (toolchain) Interpreter(string) (string, bool) {
	if p, err := exec.LookPath("php"); err == nil {
		return p, true
	}
	for _, p := range []string{"/opt/homebrew/bin/php", "/usr/local/bin/php", "/usr/bin/php"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

package langphp

import (
	"os"
	"os/exec"

	"ike/internal/lang"
)

// toolchain resolves the PHP interpreter (Roadmap 0160): PATH first, then the
// common install locations. Intelephense needs no interpreter injection, so
// Detect contributes nothing — the value feeds the settings page's display,
// the explicit [lang.php] interpreter choice, and 0170's terminal PATH shims.
type toolchain struct{}

// phpLook and phpResolve are seams for tests: PATH lookup and version-manager
// shim resolution (#650).
var (
	phpLook    = exec.LookPath
	phpResolve = lang.ResolveShim
)

// Detect implements lang.Toolchain: nothing to inject into the server.
func (toolchain) Detect(string) (map[string]any, bool) { return nil, false }

// Interpreter implements lang.InterpreterDetector. A PATH hit that is a
// version-manager shim (mise/asdf) is resolved to the real executable (#650).
func (toolchain) Interpreter(root string) (string, bool) {
	if p, err := phpLook("php"); err == nil {
		return phpResolve(root, p), true
	}
	for _, p := range []string{"/opt/homebrew/bin/php", "/usr/local/bin/php", "/usr/bin/php"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

package langpython

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ike/internal/lang"
)

// toolchain resolves the project's Python interpreter and hands its path to
// pyright. IKE only *detects* the interpreter; pyright derives the language
// version (and therefore which features are valid) from it. Detected values are
// returned nested under the "python" section so the manager can answer pyright's
// workspace/configuration request for section "python".
type toolchain struct{}

var _ lang.Toolchain = toolchain{}

func (toolchain) Detect(root string) (map[string]any, bool) {
	p, ok := interpreter(root)
	if !ok {
		return nil, false
	}
	return map[string]any{
		"python": map[string]any{
			"pythonPath":             p, // legacy key some pyright versions still read
			"defaultInterpreterPath": p,
		},
	}, true
}

// interpreter resolves the Python interpreter path in priority order: an active
// virtualenv, a project-local venv, a pyenv .python-version pin, then any python
// on PATH. ok=false means "let pyright pick its own default".
func interpreter(root string) (string, bool) {
	if v := os.Getenv("VIRTUAL_ENV"); v != "" {
		if p, ok := venvPython(v); ok {
			return p, true
		}
	}
	for _, d := range []string{".venv", "venv"} {
		if p, ok := venvPython(filepath.Join(root, d)); ok {
			return p, true
		}
	}
	if p, ok := pyenvPython(root); ok {
		return p, true
	}
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, true
		}
	}
	return "", false
}

// venvPython returns the interpreter inside a virtualenv directory, trying the
// POSIX and Windows layouts.
func venvPython(dir string) (string, bool) {
	for _, rel := range []string{"bin/python", "bin/python3", "Scripts/python.exe"} {
		p := filepath.Join(dir, rel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// pyenvPython resolves a .python-version pin against $PYENV_ROOT (default
// ~/.pyenv). It returns false when the file or the pinned version is absent.
func pyenvPython(root string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, ".python-version"))
	if err != nil {
		return "", false
	}
	// A .python-version may list several versions; the first is the active one.
	ver := strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)[0])
	if ver == "" {
		return "", false
	}
	pyroot := os.Getenv("PYENV_ROOT")
	if pyroot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		pyroot = filepath.Join(home, ".pyenv")
	}
	p := filepath.Join(pyroot, "versions", ver, "bin", "python")
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p, true
	}
	return "", false
}

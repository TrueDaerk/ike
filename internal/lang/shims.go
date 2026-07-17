package lang

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// shims.go resolves version-manager shims (#650). pyenv, mise and asdf put
// tiny dispatcher scripts on PATH (~/.pyenv/shims/python, …/mise/shims/php,
// ~/.asdf/shims/go); exec.LookPath happily returns those, hiding which real
// interpreter is active. Like JetBrains, IKE asks the owning manager for the
// actual executable so the settings page, statusline, terminal shims, debug
// launch and LSP injection all see a versioned path.

// shimRun and shimLook are seams for tests: run a manager command in a working
// directory (combined stdout) and resolve a binary on PATH.
var (
	shimRun = func(dir, name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		return string(out), err
	}
	shimLook = exec.LookPath
)

// shimManager reports which version manager owns path ("" when path is not a
// shim). Detection is by path component so it works for any install prefix
// mise uses (~/.local/share/mise/shims, XDG variants, …).
func shimManager(path string) string {
	p := filepath.ToSlash(path)
	switch {
	case strings.Contains(p, "/.pyenv/shims/"):
		return "pyenv"
	case strings.Contains(p, "/mise/shims/"):
		return "mise"
	case strings.Contains(p, "/.asdf/shims/"):
		return "asdf"
	}
	return ""
}

// ResolveShim resolves a version-manager shim to the real executable by asking
// the owning manager (`pyenv|mise|asdf which <bin>`). The manager runs with
// root as working directory so per-project pins (.python-version,
// .tool-versions, mise.toml) resolve to the project's version. Best-effort:
// a non-shim path, a missing manager binary, a failing command or an output
// that is not an existing file all return path unchanged — never worse than
// today.
func ResolveShim(root, path string) string {
	manager := shimManager(path)
	if manager == "" {
		return path
	}
	if _, err := shimLook(manager); err != nil {
		return path
	}
	out, err := shimRun(root, manager, "which", filepath.Base(path))
	if err != nil {
		return path
	}
	resolved := firstLine(out)
	if resolved == "" {
		return path
	}
	if st, err := os.Stat(resolved); err != nil || st.IsDir() {
		return path
	}
	return resolved
}

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			return l
		}
	}
	return ""
}

package terminal

import (
	"os"
	"path/filepath"
	"strings"
)

// env.go is the toolchain environment injection (Roadmap 0170, #98): the
// interpreter chosen on the settings page — and only that; silent detection
// never injects — is what `php` / `python` / `python3` resolve to inside the
// IDE terminal. A per-project shim directory holds tiny exec scripts, and
// the spawn environment prepends it to PATH plus the conventional variables
// (VIRTUAL_ENV + the venv bin for a venv choice). The shims exec their
// target by absolute path and are re-read on every invocation, so a settings
// change retargets even already-running sessions once regenerated.
//
// Windows note: these are POSIX sh shims; a windows port writes `<name>.cmd`
// wrappers into the same directory instead (`@"%target%" %*`). darwin/linux
// land first, matching the rest of the PTY stack.

// Mapping is one explicit interpreter choice from the settings page.
type Mapping struct {
	Lang        string // language id ("python", "php")
	Interpreter string // absolute interpreter path
}

// shimNames returns the command names a language's shim covers: python
// answers for both `python` and `python3`.
func shimNames(lang string) []string {
	switch lang {
	case "python":
		return []string{"python", "python3"}
	}
	return []string{lang}
}

// knownShimNames is every name WriteShims may have created, for stale sweep.
var knownShimNames = []string{"php", "python", "python3"}

// WriteShims (re)generates dir's shims for the given mappings and removes
// stale ones whose language lost its explicit setting. It reports whether
// any shim is active afterwards.
func WriteShims(dir string, mappings []Mapping) (bool, error) {
	if len(mappings) == 0 {
		for _, name := range knownShimNames {
			_ = os.Remove(filepath.Join(dir, name))
		}
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	wanted := map[string]string{}
	for _, m := range mappings {
		if m.Interpreter == "" {
			continue
		}
		for _, name := range shimNames(m.Lang) {
			wanted[name] = m.Interpreter
		}
	}
	for _, name := range knownShimNames {
		target, ok := wanted[name]
		path := filepath.Join(dir, name)
		if !ok {
			_ = os.Remove(path)
			continue
		}
		script := "#!/bin/sh\nexec \"" + target + "\" \"$@\"\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			return false, err
		}
	}
	return len(wanted) > 0, nil
}

// EnvOverlay composes the spawn-environment overrides for the mappings:
// PATH gains the shim dir (and a chosen venv's bin) up front, and a venv
// python sets VIRTUAL_ENV per convention. An empty mapping set returns nil —
// the environment stays untouched.
func EnvOverlay(shimDir string, mappings []Mapping, basePATH string) []string {
	if len(mappings) == 0 {
		return nil
	}
	prefix := []string{shimDir}
	var out []string
	for _, m := range mappings {
		if m.Lang != "python" || m.Interpreter == "" {
			continue
		}
		if venv, ok := venvRoot(m.Interpreter); ok {
			out = append(out, "VIRTUAL_ENV="+venv)
			prefix = append(prefix, filepath.Join(venv, "bin"))
		}
	}
	path := strings.Join(prefix, string(os.PathListSeparator))
	if basePATH != "" {
		path += string(os.PathListSeparator) + basePATH
	}
	return append(out, "PATH="+path)
}

// venvRoot reports the virtualenv root when interp lives in one (its bin
// directory's parent carries pyvenv.cfg).
func venvRoot(interp string) (string, bool) {
	root := filepath.Dir(filepath.Dir(interp))
	if _, err := os.Stat(filepath.Join(root, "pyvenv.cfg")); err == nil {
		return root, true
	}
	return "", false
}

// MergeEnv overlays override entries onto a base environment: keys present
// in the overlay replace the base entry (getenv implementations pick the
// first match, so duplicates are not an option).
func MergeEnv(base, overlay []string) []string {
	if len(overlay) == 0 {
		return base
	}
	replaced := map[string]bool{}
	for _, kv := range overlay {
		if i := strings.IndexByte(kv, '='); i > 0 {
			replaced[kv[:i]] = true
		}
	}
	out := make([]string, 0, len(base)+len(overlay))
	for _, kv := range base {
		if i := strings.IndexByte(kv, '='); i > 0 && replaced[kv[:i]] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, overlay...)
}

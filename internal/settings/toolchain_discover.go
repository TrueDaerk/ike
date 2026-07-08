package settings

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// toolchain_discover.go finds interpreter candidates for the toolchain page
// (#94). Discovery is best-effort and injectable: command output and PATH
// lookups go through small function seams so tests feed fixtures.

// runCommand runs a binary and returns its combined stdout ("" on any error).
type runCommand func(name string, args ...string) string

// lookPath resolves a binary on PATH ("" when absent).
type lookPath func(name string) string

// execRun is the production runCommand.
func execRun(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// execLook is the production lookPath.
func execLook(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// pythonCandidates lists Python interpreter candidates in pick order: the
// active virtualenv, project-local venvs, uv-managed interpreters, pyenv
// shims, then PATH.
func pythonCandidates(root string, run runCommand, look lookPath) []string {
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return
		}
		for _, have := range out {
			if have == p {
				return
			}
		}
		out = append(out, p)
	}
	if v := os.Getenv("VIRTUAL_ENV"); v != "" {
		add(filepath.Join(v, "bin", "python"))
	}
	for _, d := range []string{".venv", "venv"} {
		add(filepath.Join(root, d, "bin", "python"))
	}
	for _, p := range parseUvPythonList(run("uv", "python", "list")) {
		add(p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".pyenv", "shims", "python"))
	}
	add(look("python3"))
	add(look("python"))
	return out
}

// parseUvPythonList extracts installed interpreter paths from `uv python
// list` output: one entry per line, "<key>  <path>"; uninstalled versions show
// "<download available>" instead of a path and are skipped.
func parseUvPythonList(out string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		p := fields[len(fields)-1]
		if strings.HasPrefix(p, string(filepath.Separator)) {
			paths = append(paths, p)
		}
	}
	return paths
}

// phpCandidates lists PHP interpreter candidates: PATH first, then common
// install locations.
func phpCandidates(look lookPath) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(look("php"))
	for _, p := range []string{"/opt/homebrew/bin/php", "/usr/local/bin/php", "/usr/bin/php"} {
		add(p)
	}
	return out
}

// versionArgs returns the probe invocation for a language's interpreter.
func versionArgs(langID string) []string {
	if langID == "php" {
		return []string{"-v"}
	}
	return []string{"--version"}
}

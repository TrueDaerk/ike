package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// pythonenv_info.go answers "what is this environment?" for the toolchain
// page (#569): how the effective interpreter's environment was created
// (provenance) and which packages it carries. Both mirror what PyCharm's
// interpreter settings surface. Provenance is derived from pyvenv.cfg — uv
// stamps a `uv = <version>` key into environments it creates, a stdlib venv
// does not — with path heuristics for interpreters outside any environment.

// envProvenance classifies how a python interpreter's environment was
// created: "uv venv", "venv", "uv managed", "pyenv" or "system".
func envProvenance(interp string) string {
	if interp == "" {
		return ""
	}
	// venv layouts put the binary one level below the env root: <env>/bin/
	// python (POSIX) or <env>\Scripts\python.exe (Windows); pyvenv.cfg sits
	// at the root.
	envDir := filepath.Dir(filepath.Dir(interp))
	if data, err := os.ReadFile(filepath.Join(envDir, "pyvenv.cfg")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			key, _, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(key) == "uv" {
				return "uv venv"
			}
		}
		return "venv"
	}
	sep := string(filepath.Separator)
	switch {
	case strings.Contains(interp, sep+"uv"+sep+"python"+sep):
		return "uv managed"
	case strings.Contains(interp, sep+".pyenv"+sep):
		return "pyenv"
	}
	return "system"
}

// pkgInfo is one installed package.
type pkgInfo struct {
	Name    string
	Version string
}

// PackagesMsg delivers an async package listing for an interpreter. The root
// model forwards it to the settings panel (Model.Deliver).
type PackagesMsg struct {
	Path string
	Pkgs []pkgInfo
	Err  error
}

// listPackages builds the async package-listing command for an interpreter:
// `uv pip list` when uv is on PATH (works even in envs without pip), the
// interpreter's own pip module otherwise.
func listPackages(interp string, run runCommand, look lookPath) tea.Cmd {
	return func() tea.Msg {
		var out string
		if look("uv") != "" {
			out = run("uv", "pip", "list", "--python", interp, "--format", "freeze")
		}
		if strings.TrimSpace(out) == "" {
			out = run(interp, "-m", "pip", "list", "--format=freeze")
		}
		pkgs := parseFreeze(out)
		if len(pkgs) == 0 {
			return PackagesMsg{Path: interp, Err: fmt.Errorf("no packages reported (pip missing in this environment?)")}
		}
		return PackagesMsg{Path: interp, Pkgs: pkgs}
	}
}

// parseFreeze parses pip/uv freeze-format output (`name==version` per line)
// into packages; editable/local installs (`name @ path`) show "(local)".
func parseFreeze(out string) []pkgInfo {
	var pkgs []pkgInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-e ") {
			continue
		}
		if name, version, ok := strings.Cut(line, "=="); ok {
			pkgs = append(pkgs, pkgInfo{Name: name, Version: version})
			continue
		}
		if name, _, ok := strings.Cut(line, " @ "); ok {
			pkgs = append(pkgs, pkgInfo{Name: strings.TrimSpace(name), Version: "(local)"})
		}
	}
	return pkgs
}

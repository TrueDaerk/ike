package settings

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// pythonenv.go is the Python environment management half of the toolchain
// page (Roadmap 0180, #132): create a project venv (`uv venv` when uv is on
// PATH, `python -m venv .venv` otherwise) and install a managed Python via
// `uv python install <version>` picked from `uv python list`. Both run
// asynchronously inside tea.Cmds; the result lands as an EnvMsg the root
// model routes — it registers the new interpreter through the write-back
// layer ([lang.python] interpreter) and offers the LSP restart, so
// lang.Interpreter stays the single source of truth.

// EnvMsg reports a finished environment action. Interpreter is the python
// binary to register (empty when the action failed); Label phrases the
// result for the toast.
type EnvMsg struct {
	LangID      string
	Interpreter string
	Label       string
	Err         error
}

// envBusy is the in-flight marker the view shows while an action runs.
const envBusy = "working…"

// createEnv builds the async create-environment command for the project
// root: uv when present, the stdlib venv module otherwise.
func createEnv(root string, run runCommand, look lookPath) tea.Cmd {
	venv := filepath.Join(root, ".venv")
	// Register an absolute interpreter path: the effective root is often "."
	// and lang.Interpreter/server launches should not depend on the CWD.
	if abs, err := filepath.Abs(venv); err == nil {
		venv = abs
	}
	interp := filepath.Join(venv, "bin", "python")
	return func() tea.Msg {
		switch {
		case look("uv") != "":
			run("uv", "venv", venv)
		case look("python3") != "":
			run("python3", "-m", "venv", venv)
		case look("python") != "":
			run("python", "-m", "venv", venv)
		default:
			return EnvMsg{LangID: "python", Err: fmt.Errorf("neither uv nor python found on PATH")}
		}
		if !fileExists(interp) {
			return EnvMsg{LangID: "python", Err: fmt.Errorf("environment creation left no interpreter at %s", interp)}
		}
		return EnvMsg{LangID: "python", Interpreter: interp, Label: "created " + venv}
	}
}

// uvInstallable parses `uv python list` output into the versions offered for
// download (installed builds carry a path instead of the download marker).
func uvInstallable(listOutput string) []string {
	var out []string
	for _, line := range strings.Split(listOutput, "\n") {
		if !strings.Contains(line, "<download available>") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// cpython-3.12.8-macos-aarch64-none -> 3.12.8
		parts := strings.Split(fields[0], "-")
		if len(parts) >= 2 && parts[0] == "cpython" {
			out = append(out, parts[1])
		}
	}
	return out
}

// uvInstall builds the async install command for a picked version: install,
// then resolve the managed interpreter's path for registration.
func uvInstall(version string, run runCommand) tea.Cmd {
	return func() tea.Msg {
		run("uv", "python", "install", version)
		path := strings.TrimSpace(run("uv", "python", "find", version))
		if i := strings.IndexByte(path, '\n'); i >= 0 {
			path = path[:i]
		}
		if path == "" || !fileExists(path) {
			return EnvMsg{LangID: "python", Err: fmt.Errorf("uv python install %s did not yield an interpreter", version)}
		}
		return EnvMsg{LangID: "python", Interpreter: path, Label: "installed python " + version}
	}
}

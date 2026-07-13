package settings

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pathcomplete"
)

// pythonenv.go is the Python environment management half of the toolchain
// page (Roadmap 0180, #132): create a venv (`uv venv` when uv is on PATH,
// `python -m venv` otherwise) at a chosen target (#547) and install a managed
// Python via `uv python install <version>` picked from `uv python list`. On
// the uv path the project is scaffolded too (#548): a missing pyproject.toml
// is generated via `uv init --bare` and a missing uv.lock via `uv lock` —
// best effort, existing files are never touched. Both actions run
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

// envSpec captures the guided create-wizard's choices (#569): the tool
// ("uv" or "venv"), the Python to build on (uv: a version, "" = uv's
// default; venv: the base interpreter path, "" = python3/python from PATH)
// and the target directory.
type envSpec struct {
	Tool   string
	Python string
	Target string
}

// createEnv builds the async create-environment command with automatic tool
// selection: uv when present, the stdlib venv module otherwise. target is
// where the environment lands (#547).
func createEnv(root, target string, run runCommand, look lookPath) tea.Cmd {
	tool := "venv"
	if look("uv") != "" {
		tool = "uv"
	}
	return createEnvWith(root, envSpec{Tool: tool, Target: target}, run, look)
}

// createEnvWith builds the async create-environment command for an explicit
// spec (#569). Relative targets resolve against the project root, ~ expands.
func createEnvWith(root string, spec envSpec, run runCommand, look lookPath) tea.Cmd {
	venv := pathcomplete.Expand(spec.Target)
	if !filepath.IsAbs(venv) {
		venv = filepath.Join(root, venv)
	}
	// Register an absolute interpreter path: the effective root is often "."
	// and lang.Interpreter/server launches should not depend on the CWD.
	if abs, err := filepath.Abs(venv); err == nil {
		venv = abs
	}
	interp := filepath.Join(venv, "bin", "python")
	return func() tea.Msg {
		var scaffolded []string
		detail := spec.Tool
		switch spec.Tool {
		case "uv":
			if look("uv") == "" {
				return EnvMsg{LangID: "python", Err: fmt.Errorf("uv not found on PATH")}
			}
			scaffolded = uvScaffold(root, run) // pyproject.toml before the env (#548)
			args := []string{"venv"}
			if spec.Python != "" {
				args = append(args, "--python", spec.Python)
				detail += ", python " + spec.Python
			}
			run("uv", append(args, venv)...)
		default:
			base := spec.Python
			if base == "" {
				for _, name := range []string{"python3", "python"} {
					if look(name) != "" {
						base = name
						break
					}
				}
			}
			if base == "" {
				return EnvMsg{LangID: "python", Err: fmt.Errorf("neither uv nor python found on PATH")}
			}
			detail = base + " -m venv"
			run(base, "-m", "venv", venv)
		}
		if !fileExists(interp) {
			return EnvMsg{LangID: "python", Err: fmt.Errorf("environment creation left no interpreter at %s", interp)}
		}
		if spec.Tool == "uv" {
			scaffolded = append(scaffolded, uvLock(root, run)...)
		}
		label := "created " + venv + " (" + detail + ")"
		for _, s := range scaffolded {
			label += " + " + s
		}
		return EnvMsg{LangID: "python", Interpreter: interp, Label: label}
	}
}

// uvScaffold makes the project a uv project before the env is created
// (#548): a missing pyproject.toml is generated with `uv init --bare` (the
// manifest only — no sample sources). An existing manifest is never touched.
// It returns the names of the files it created (best effort — a failing uv
// simply creates nothing).
func uvScaffold(root string, run runCommand) []string {
	if fileExists(filepath.Join(root, "pyproject.toml")) {
		return nil
	}
	run("uv", "init", "--bare", root)
	if !fileExists(filepath.Join(root, "pyproject.toml")) {
		return nil
	}
	return []string{"pyproject.toml"}
}

// uvLock generates the lockfile for the project's manifest when both uv and
// pyproject.toml are present and no uv.lock exists yet (#548). Best effort:
// the created env stands regardless of the lock outcome.
func uvLock(root string, run runCommand) []string {
	if !fileExists(filepath.Join(root, "pyproject.toml")) || fileExists(filepath.Join(root, "uv.lock")) {
		return nil
	}
	run("uv", "lock", "--directory", root)
	if !fileExists(filepath.Join(root, "uv.lock")) {
		return nil
	}
	return []string{"uv.lock"}
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

// uvVersionsAll parses `uv python list` output into every cpython version uv
// can provide — installed or downloadable — deduplicated in list order. The
// create wizard (#569) offers these for `uv venv --python <version>` (uv
// downloads a missing version on demand).
func uvVersionsAll(listOutput string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(listOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// cpython-3.12.8-macos-aarch64-none -> 3.12.8
		parts := strings.Split(fields[0], "-")
		if len(parts) >= 2 && parts[0] == "cpython" && !seen[parts[1]] {
			seen[parts[1]] = true
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

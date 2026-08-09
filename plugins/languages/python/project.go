package langpython

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ike/internal/lang"
)

// project.go implements lang.ProjectScaffolder (#1718): Python appears as a
// project type in the new-project wizard with a guided virtual-environment
// setup — uv when it is on PATH (pyproject.toml + uv.lock + .venv via
// `uv init` and `uv sync`), the stdlib venv module otherwise (`python -m venv
// .venv` plus a main.py seed). Both options are always listed; an unavailable
// one is disabled with the reason, mirroring the settings venv wizard (#884).

// pyProjRun is the subprocess seam for tests: run name args… in dir and
// return the combined output.
var pyProjRun = func(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ProjectOptions implements lang.ProjectScaffolder: cheap PATH probes only.
func (toolchain) ProjectOptions() []lang.ProjectOption {
	uv, uvErr := pyLook("uv")
	py := ""
	for _, name := range []string{"python3", "python"} {
		if p, err := pyLook(name); err == nil {
			py = p
			break
		}
	}
	return []lang.ProjectOption{{
		ID:        "uv",
		Label:     "uv — pyproject.toml, uv.lock + .venv",
		Detail:    uv,
		Available: uvErr == nil,
		Reason:    "uv not found on PATH",
	}, {
		ID:        "venv",
		Label:     "pip / venv — stdlib virtual environment",
		Detail:    py,
		Available: py != "",
		Reason:    "no python on PATH",
	}}
}

// ScaffoldProject implements lang.ProjectScaffolder: populate the fresh
// directory at root and create its virtual environment. The interpreter check
// at the end guards both paths — a project whose venv silently failed would
// defeat the wizard's purpose.
func (toolchain) ScaffoldProject(root, option string) error {
	switch option {
	case "uv":
		// `uv init` scaffolds pyproject.toml + main.py, `uv sync` materializes
		// .venv and uv.lock from the manifest.
		for _, args := range [][]string{{"init"}, {"sync"}} {
			if out, err := pyProjRun(root, "uv", args...); err != nil {
				return scaffoldError("uv "+strings.Join(args, " "), out, err)
			}
		}
	case "venv":
		base := ""
		for _, name := range []string{"python3", "python"} {
			if p, err := pyLook(name); err == nil {
				base = p
				break
			}
		}
		if base == "" {
			return fmt.Errorf("no python on PATH")
		}
		if out, err := pyProjRun(root, base, "-m", "venv", filepath.Join(root, ".venv")); err != nil {
			return scaffoldError(base+" -m venv", out, err)
		}
		if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(pythonMain), 0o644); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown python project option %q", option)
	}
	if _, ok := venvPython(filepath.Join(root, ".venv")); !ok {
		return fmt.Errorf("scaffold left no interpreter in %s", filepath.Join(root, ".venv"))
	}
	return nil
}

// pythonMain seeds the venv option's entry point; uv writes its own.
const pythonMain = `def main():
    print("Hello from your new project!")


if __name__ == "__main__":
    main()
`

// scaffoldError attaches the tail of the tool output to the error, so the
// wizard shows the real reason instead of "exit status 1".
func scaffoldError(cmdline, out string, err error) error {
	out = strings.TrimSpace(out)
	if out == "" {
		return fmt.Errorf("%s failed: %v", cmdline, err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return fmt.Errorf("%s failed: %v — %s", cmdline, err, strings.Join(lines, " · "))
}

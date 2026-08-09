package langpython

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubProjRun records scaffold subprocess calls and simulates the venv the
// real tools would create.
func stubProjRun(t *testing.T, calls *[]string, makeVenv bool) {
	t.Helper()
	prev := pyProjRun
	t.Cleanup(func() { pyProjRun = prev })
	pyProjRun = func(dir, name string, args ...string) (string, error) {
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		if makeVenv {
			bin := filepath.Join(dir, ".venv", "bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bin, "python"), []byte("#!"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return "", nil
	}
}

// stubProjLook points the PATH probe at fixed results.
func stubProjLook(t *testing.T, hits map[string]string) {
	t.Helper()
	prev := pyLook
	t.Cleanup(func() { pyLook = prev })
	pyLook = func(name string) (string, error) {
		if p, ok := hits[name]; ok {
			return p, nil
		}
		return "", os.ErrNotExist
	}
}

// TestProjectOptionsAvailability guards the wizard rows: both options always
// listed, availability and detail from the PATH probes.
func TestProjectOptionsAvailability(t *testing.T) {
	stubProjLook(t, map[string]string{"uv": "/opt/uv", "python3": "/usr/bin/python3"})
	opts := toolchain{}.ProjectOptions()
	if len(opts) != 2 || opts[0].ID != "uv" || opts[1].ID != "venv" {
		t.Fatalf("options = %+v", opts)
	}
	if !opts[0].Available || opts[0].Detail != "/opt/uv" {
		t.Fatalf("uv option = %+v", opts[0])
	}
	if !opts[1].Available || opts[1].Detail != "/usr/bin/python3" {
		t.Fatalf("venv option = %+v", opts[1])
	}

	stubProjLook(t, map[string]string{"python3": "/usr/bin/python3"})
	opts = toolchain{}.ProjectOptions()
	if opts[0].Available || opts[0].Reason == "" {
		t.Fatalf("missing uv must disable the option with a reason, got %+v", opts[0])
	}
	if !opts[1].Available {
		t.Fatalf("venv must stay available, got %+v", opts[1])
	}
}

// TestScaffoldUVRunsInitAndSync guards the uv path: init then sync, verified
// against the created venv.
func TestScaffoldUVRunsInitAndSync(t *testing.T) {
	root := t.TempDir()
	var calls []string
	stubProjRun(t, &calls, true)
	stubProjLook(t, map[string]string{"uv": "/opt/uv"})

	if err := (toolchain{}).ScaffoldProject(root, "uv"); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	want := []string{"uv init", "uv sync"}
	if len(calls) != 2 || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

// TestScaffoldVenvCreatesEnvAndMain guards the stdlib path: python -m venv
// plus the main.py seed.
func TestScaffoldVenvCreatesEnvAndMain(t *testing.T) {
	root := t.TempDir()
	var calls []string
	stubProjRun(t, &calls, true)
	stubProjLook(t, map[string]string{"python3": "/usr/bin/python3"})

	if err := (toolchain{}).ScaffoldProject(root, "venv"); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	want := "/usr/bin/python3 -m venv " + filepath.Join(root, ".venv")
	if len(calls) != 1 || calls[0] != want {
		t.Fatalf("calls = %v, want [%s]", calls, want)
	}
	if _, err := os.Stat(filepath.Join(root, "main.py")); err != nil {
		t.Fatalf("main.py not seeded: %v", err)
	}
}

// TestScaffoldReportsMissingInterpreter guards the final check: a scaffold
// that leaves no venv interpreter is an error, not a silent half-project.
func TestScaffoldReportsMissingInterpreter(t *testing.T) {
	root := t.TempDir()
	var calls []string
	stubProjRun(t, &calls, false) // tools "succeed" but create nothing
	stubProjLook(t, map[string]string{"uv": "/opt/uv"})

	if err := (toolchain{}).ScaffoldProject(root, "uv"); err == nil {
		t.Fatal("a venv-less scaffold must error")
	}
}

// TestScaffoldErrorCarriesOutput guards the failure message: the tool's
// output tail rides on the error.
func TestScaffoldErrorCarriesOutput(t *testing.T) {
	root := t.TempDir()
	prev := pyProjRun
	t.Cleanup(func() { pyProjRun = prev })
	pyProjRun = func(dir, name string, args ...string) (string, error) {
		return "error: no interpreter found for request", errors.New("exit status 2")
	}
	stubProjLook(t, map[string]string{"uv": "/opt/uv"})

	err := (toolchain{}).ScaffoldProject(root, "uv")
	if err == nil || !strings.Contains(err.Error(), "no interpreter found") {
		t.Fatalf("error must carry the tool output, got %v", err)
	}
}

// TestScaffoldRejectsUnknownOption keeps the option ids closed.
func TestScaffoldRejectsUnknownOption(t *testing.T) {
	if err := (toolchain{}).ScaffoldProject(t.TempDir(), "conda"); err == nil {
		t.Fatal("unknown option must refuse")
	}
}

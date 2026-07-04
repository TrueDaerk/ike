package langpython

import (
	"os"
	"path/filepath"
	"testing"
)

// mkPython creates a fake interpreter file at dir/bin/python and returns its path.
func mkPython(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(bin, "python")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func pythonPath(t *testing.T, settings map[string]any) string {
	t.Helper()
	py, ok := settings["python"].(map[string]any)
	if !ok {
		t.Fatalf("no python section in %v", settings)
	}
	return py["defaultInterpreterPath"].(string)
}

func TestDetectProjectVenv(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "") // ensure no active venv leaks in
	root := t.TempDir()
	want := mkPython(t, filepath.Join(root, ".venv"))

	settings, ok := toolchain{}.Detect(root)
	if !ok {
		t.Fatal("expected detection from .venv")
	}
	if got := pythonPath(t, settings); got != want {
		t.Errorf("interpreter = %q, want %q", got, want)
	}
}

func TestActiveVirtualenvWins(t *testing.T) {
	venv := t.TempDir()
	want := mkPython(t, venv)
	t.Setenv("VIRTUAL_ENV", venv)

	root := t.TempDir()
	mkPython(t, filepath.Join(root, ".venv")) // present but must lose to $VIRTUAL_ENV

	settings, ok := toolchain{}.Detect(root)
	if !ok {
		t.Fatal("expected detection from $VIRTUAL_ENV")
	}
	if got := pythonPath(t, settings); got != want {
		t.Errorf("interpreter = %q, want active venv %q", got, want)
	}
}

func TestPyenvVersionPin(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "")
	pyroot := t.TempDir()
	t.Setenv("PYENV_ROOT", pyroot)
	want := mkPython(t, filepath.Join(pyroot, "versions", "3.12.1"))

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".python-version"), []byte("3.12.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	settings, ok := toolchain{}.Detect(root)
	if !ok {
		t.Fatal("expected detection from .python-version")
	}
	if got := pythonPath(t, settings); got != want {
		t.Errorf("interpreter = %q, want pyenv %q", got, want)
	}
}

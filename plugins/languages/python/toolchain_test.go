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

// TestPathHitResolvesShims guards #650: with no venv or pyenv pin, a PATH hit
// that is a version-manager shim resolves to the real interpreter.
func TestPathHitResolvesShims(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "")
	prevLook, prevResolve := pyLook, pyResolve
	t.Cleanup(func() { pyLook, pyResolve = prevLook, prevResolve })

	root := t.TempDir() // no venv, no .python-version
	pyLook = func(name string) (string, error) {
		if name == "python3" {
			return "/home/u/.pyenv/shims/python3", nil
		}
		return "", os.ErrNotExist
	}
	pyResolve = func(gotRoot, p string) string {
		if gotRoot != root {
			t.Errorf("resolve root = %q, want %q", gotRoot, root)
		}
		return "/home/u/.pyenv/versions/3.12.4/bin/python3.12"
	}
	p, ok := interpreter(root)
	if !ok || p != "/home/u/.pyenv/versions/3.12.4/bin/python3.12" {
		t.Fatalf("interpreter = %q %v, want resolved shim", p, ok)
	}
}

// TestVenvNotShimResolved guards #650: venv interpreters are real paths and
// must not be passed through the shim resolver.
func TestVenvNotShimResolved(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "")
	prevResolve := pyResolve
	t.Cleanup(func() { pyResolve = prevResolve })
	pyResolve = func(_, p string) string {
		t.Errorf("resolver must not run for venv path %q", p)
		return p
	}

	root := t.TempDir()
	want := mkPython(t, filepath.Join(root, ".venv"))
	if p, ok := interpreter(root); !ok || p != want {
		t.Fatalf("interpreter = %q %v, want %q", p, ok, want)
	}
}

package lang

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// stubShims installs fake shimRun/shimLook seams and restores them on cleanup.
func stubShims(t *testing.T, run func(dir, name string, args ...string) (string, error), look func(string) (string, error)) {
	t.Helper()
	prevRun, prevLook := shimRun, shimLook
	t.Cleanup(func() { shimRun, shimLook = prevRun, prevLook })
	shimRun, shimLook = run, look
}

// mkExe creates a fake executable and returns its path.
func mkExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveShimByManager(t *testing.T) {
	real := mkExe(t, t.TempDir(), "python3.12")
	cases := []struct {
		shim, manager string
	}{
		{"/home/u/.pyenv/shims/python", "pyenv"},
		{"/home/u/.local/share/mise/shims/python", "mise"},
		{"/home/u/.asdf/shims/python", "asdf"},
	}
	for _, tc := range cases {
		var gotDir, gotName string
		var gotArgs []string
		stubShims(t,
			func(dir, name string, args ...string) (string, error) {
				gotDir, gotName, gotArgs = dir, name, args
				return real + "\n", nil
			},
			func(string) (string, error) { return "/usr/bin/" + tc.manager, nil },
		)
		if got := ResolveShim("/proj", tc.shim); got != real {
			t.Errorf("ResolveShim(%q) = %q, want %q", tc.shim, got, real)
		}
		if gotName != tc.manager {
			t.Errorf("manager for %q = %q, want %q", tc.shim, gotName, tc.manager)
		}
		if gotDir != "/proj" {
			t.Errorf("cwd = %q, want project root (per-project pins)", gotDir)
		}
		if len(gotArgs) != 2 || gotArgs[0] != "which" || gotArgs[1] != "python" {
			t.Errorf("args = %v, want [which python]", gotArgs)
		}
	}
}

func TestResolveShimNonShimPassthrough(t *testing.T) {
	stubShims(t,
		func(string, string, ...string) (string, error) {
			t.Fatal("manager must not be invoked for a non-shim path")
			return "", nil
		},
		func(string) (string, error) {
			t.Fatal("no PATH lookup for a non-shim path")
			return "", nil
		},
	)
	for _, p := range []string{"/usr/bin/python3", "/opt/homebrew/bin/php", ""} {
		if got := ResolveShim("/proj", p); got != p {
			t.Errorf("ResolveShim(%q) = %q, want unchanged", p, got)
		}
	}
}

func TestResolveShimFallsBackToShim(t *testing.T) {
	shim := "/home/u/.pyenv/shims/python"

	// Manager binary missing on PATH.
	stubShims(t,
		func(string, string, ...string) (string, error) { return "", nil },
		func(string) (string, error) { return "", errors.New("not found") },
	)
	if got := ResolveShim("/proj", shim); got != shim {
		t.Errorf("missing manager: got %q, want shim unchanged", got)
	}

	look := func(string) (string, error) { return "/usr/bin/pyenv", nil }

	// Command fails.
	stubShims(t, func(string, string, ...string) (string, error) {
		return "", errors.New("pyenv: python not installed")
	}, look)
	if got := ResolveShim("/proj", shim); got != shim {
		t.Errorf("command error: got %q, want shim unchanged", got)
	}

	// Empty output.
	stubShims(t, func(string, string, ...string) (string, error) { return "\n  \n", nil }, look)
	if got := ResolveShim("/proj", shim); got != shim {
		t.Errorf("empty output: got %q, want shim unchanged", got)
	}

	// Output is not an existing file.
	stubShims(t, func(string, string, ...string) (string, error) {
		return filepath.Join(t.TempDir(), "gone", "python") + "\n", nil
	}, look)
	if got := ResolveShim("/proj", shim); got != shim {
		t.Errorf("nonexistent output: got %q, want shim unchanged", got)
	}

	// Output is a directory.
	stubShims(t, func(string, string, ...string) (string, error) { return t.TempDir() + "\n", nil }, look)
	if got := ResolveShim("/proj", shim); got != shim {
		t.Errorf("directory output: got %q, want shim unchanged", got)
	}
}

func TestResolveShimTrimsToFirstLine(t *testing.T) {
	real := mkExe(t, t.TempDir(), "php")
	stubShims(t,
		func(string, string, ...string) (string, error) {
			return "\n  " + real + "  \nmise: extra noise\n", nil
		},
		func(string) (string, error) { return "/usr/bin/mise", nil },
	)
	if got := ResolveShim("/proj", "/home/u/.local/share/mise/shims/php"); got != real {
		t.Errorf("ResolveShim = %q, want first-line %q", got, real)
	}
}

package langpython

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lang"
)

// mkVenv builds a minimal venv layout under dir and returns the interpreter
// and site-packages paths.
func mkVenv(t *testing.T, dir, pyver string) (python, site string) {
	t.Helper()
	python = filepath.Join(dir, "bin", "python")
	site = filepath.Join(dir, "lib", pyver, "site-packages")
	if err := os.MkdirAll(filepath.Dir(python), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	return python, site
}

// TestSitePackagesFollowsDetectedVenv guards #2613: the marker directory is
// derived from the very interpreter the toolchain hands pyright, so the
// watched tree is the one the server actually reads.
func TestSitePackagesFollowsDetectedVenv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VIRTUAL_ENV", "")
	_, site := mkVenv(t, filepath.Join(root, ".venv"), "python3.12")

	got := sitePackages(root)
	if len(got) != 1 || got[0] != site {
		t.Fatalf("sitePackages(%s) = %v, want [%s]", root, got, site)
	}
}

// TestSitePackagesSkipsMissingLayout: a project without a venv contributes no
// marker directory at all — the static manifests still do.
func TestSitePackagesSkipsMissingLayout(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VIRTUAL_ENV", "")
	// A venv whose lib/ holds no site-packages.
	python := filepath.Join(root, ".venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(python), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := sitePackages(root); len(got) != 0 {
		t.Fatalf("sitePackages = %v, want none", got)
	}
}

// TestPythonDeclaresDepMarkers pins the registered declaration: the manifests
// Python owns, the site-packages resolver, and the restart flag that sets
// pyright apart from the notify-only servers.
func TestPythonDeclaresDepMarkers(t *testing.T) {
	l, ok := lang.ByID("python")
	if !ok || l.Deps == nil {
		t.Fatal("python declares no dependency markers")
	}
	want := map[string]bool{
		"requirements*.txt": true,
		"pyproject.toml":    true,
		"uv.lock":           true,
		"poetry.lock":       true,
	}
	for _, f := range l.Deps.Files {
		delete(want, f)
	}
	if len(want) != 0 {
		t.Errorf("missing markers %v in %v", want, l.Deps.Files)
	}
	if l.Deps.Dirs == nil {
		t.Error("python must resolve its site-packages directory")
	}
	if !l.Deps.Restart {
		t.Error("python must restart on a dependency change: pyright does not re-scan site-packages")
	}
}

// TestSitePackagesIgnoresPATHPython guards the cost rule: without a project
// interpreter the language contributes no marker directory at all. The PATH
// fallback that Toolchain.Detect ends on is a machine default, and following
// it would make every non-Python project on the box watch the system
// site-packages.
func TestSitePackagesIgnoresPATHPython(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VIRTUAL_ENV", "")
	if got := sitePackages(root); len(got) != 0 {
		t.Fatalf("sitePackages without a project interpreter = %v, want none", got)
	}
	// The toolchain detector still resolves *something* for pyright here —
	// that is the difference this test pins.
	if _, ok := interpreter(root); !ok {
		t.Skip("no python on PATH: nothing to distinguish from")
	}
}

// TestSitePackagesFollowsActiveVirtualenv: an activated venv outside the
// project root is the interpreter pyright gets, so it is the tree to watch.
func TestSitePackagesFollowsActiveVirtualenv(t *testing.T) {
	root := t.TempDir()
	venv := t.TempDir()
	_, site := mkVenv(t, venv, "python3.11")
	t.Setenv("VIRTUAL_ENV", venv)

	got := sitePackages(root)
	if len(got) != 1 || got[0] != site {
		t.Fatalf("sitePackages = %v, want [%s]", got, site)
	}
}

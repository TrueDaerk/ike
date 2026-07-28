package project

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/config"
)

// TestResolveDirDefaults verifies that an unset setting selects
// ~/DefaultDirectory instead of the working directory or $HOME itself.
func TestResolveDirDefaults(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("home directory unknown")
	}
	for _, in := range []string{"", "   "} {
		got, err := resolveDir(in)
		if err != nil {
			t.Fatalf("resolveDir(%q): %v", in, err)
		}
		if want := filepath.Join(home, DefaultDirectory); got != want {
			t.Fatalf("resolveDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestResolveDirExpandsTilde verifies the ~ expansion and that an explicit
// absolute path is returned cleaned.
func TestResolveDirExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("home directory unknown")
	}
	got, err := resolveDir("~/Code/repos")
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}
	if want := filepath.Join(home, "Code", "repos"); got != want {
		t.Fatalf("tilde expansion = %q, want %q", got, want)
	}

	dir := t.TempDir()
	got, err = resolveDir(dir + "/nested/../nested")
	if err != nil {
		t.Fatalf("resolveDir: %v", err)
	}
	if want := filepath.Join(dir, "nested"); got != want {
		t.Fatalf("cleaned path = %q, want %q", got, want)
	}
}

// TestProjectsDirFollowsConfig verifies that the resolution reads the live
// [project] directory setting.
func TestProjectsDirFollowsConfig(t *testing.T) {
	dir := t.TempDir()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := &config.Config{}
	c.Project.Directory = dir
	config.Set(c)

	got, err := ProjectsDir()
	if err != nil {
		t.Fatalf("ProjectsDir: %v", err)
	}
	if got != dir {
		t.Fatalf("ProjectsDir = %q, want %q", got, dir)
	}
}

// TestEnsureDirectoryCreatesAndIsIdempotent verifies that the directory is
// created on demand, that a second call is a no-op and that a file in its
// place is reported instead of silently used.
func TestEnsureDirectoryCreatesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "projects", "nested")
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := &config.Config{}
	c.Project.Directory = target
	config.Set(c)

	for i := range 2 {
		got, err := EnsureDirectory()
		if err != nil {
			t.Fatalf("EnsureDirectory call %d: %v", i, err)
		}
		if got != target {
			t.Fatalf("EnsureDirectory = %q, want %q", got, target)
		}
		info, err := os.Stat(target)
		if err != nil || !info.IsDir() {
			t.Fatalf("call %d did not leave a directory at %s (%v)", i, target, err)
		}
	}

	file := filepath.Join(root, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Project.Directory = file
	config.Set(c)
	if _, err := EnsureDirectory(); err == nil {
		t.Fatal("EnsureDirectory accepted a file as the project directory")
	}
}

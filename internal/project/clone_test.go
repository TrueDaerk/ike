package project

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/config"
)

// projectsDir points the project directory at a fresh temp dir for one test.
func projectsDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "projects")
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := &config.Config{}
	c.Project.Directory = dir
	config.Set(c)
	return dir
}

// TestCloneTargetCreatesProjectDirectory verifies the happy path: the target
// is the project directory joined with the name, and the directory itself is
// created on demand.
func TestCloneTargetCreatesProjectDirectory(t *testing.T) {
	dir := projectsDir(t)
	got, err := CloneTarget("  ike  ")
	if err != nil {
		t.Fatalf("CloneTarget: %v", err)
	}
	if want := filepath.Join(dir, "ike"); got != want {
		t.Fatalf("CloneTarget = %q, want %q", got, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("project directory not created (%v)", err)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("CloneTarget must not create the target itself (%v)", err)
	}
}

// TestCloneTargetRejectsBadNames covers the validation git would otherwise hit
// halfway through: empty names, path segments and an occupied target.
func TestCloneTargetRejectsBadNames(t *testing.T) {
	dir := projectsDir(t)
	for _, name := range []string{"", "   ", ".", "..", "org/repo", "nested" + string(filepath.Separator) + "repo"} {
		if _, err := CloneTarget(name); err == nil {
			t.Errorf("CloneTarget(%q) was accepted", name)
		}
	}

	if err := os.MkdirAll(filepath.Join(dir, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CloneTarget("taken"); err == nil {
		t.Fatal("CloneTarget accepted an existing directory")
	}
}

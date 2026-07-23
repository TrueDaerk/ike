package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/config"
)

// seedRestore writes a user config with project.restore_last=on and records
// root as the most recent open.
func seedRestore(t *testing.T, root string) config.Options {
	t.Helper()
	o := testOpts(t)
	if err := os.WriteFile(o.UserPath, []byte("[project]\nrestore_last = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RecordOpen(o, root, time.Now()); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestRestoreLastRootReturnsLastProject(t *testing.T) {
	last := t.TempDir()
	opts := seedRestore(t, last)
	root, notice := RestoreLastRoot(opts, filepath.Join(t.TempDir(), "elsewhere"))
	if notice != "" {
		t.Fatalf("unexpected notice %q", notice)
	}
	want, _ := Validate(last)
	if root != want {
		t.Fatalf("root=%q want %q", root, want)
	}
}

func TestRestoreLastRootDisabledOrEmpty(t *testing.T) {
	// Setting off: history exists but nothing restores.
	off := testOpts(t)
	if err := RecordOpen(off, t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if root, notice := RestoreLastRoot(off, "/nowhere"); root != "" || notice != "" {
		t.Fatalf("restore_last=off must be a no-op, got %q/%q", root, notice)
	}
	// Setting on, empty history.
	on := testOpts(t)
	if err := os.WriteFile(on.UserPath, []byte("[project]\nrestore_last = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if root, notice := RestoreLastRoot(on, "/nowhere"); root != "" || notice != "" {
		t.Fatalf("empty history must be a no-op, got %q/%q", root, notice)
	}
}

func TestRestoreLastRootSkipsCurrentDir(t *testing.T) {
	last := t.TempDir()
	opts := seedRestore(t, last)
	abs, _ := Validate(last)
	if root, notice := RestoreLastRoot(opts, abs); root != "" || notice != "" {
		t.Fatalf("cwd == last project must be a no-op, got %q/%q", root, notice)
	}
}

func TestRestoreLastRootMissingDirFallsBack(t *testing.T) {
	last := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(last, 0o755); err != nil {
		t.Fatal(err)
	}
	opts := seedRestore(t, last)
	if err := os.RemoveAll(last); err != nil {
		t.Fatal(err)
	}
	root, notice := RestoreLastRoot(opts, "/somewhere")
	if root != "" {
		t.Fatalf("deleted project must not restore, got %q", root)
	}
	if notice == "" {
		t.Fatal("deleted project must raise a fallback notice")
	}
}

func TestRestoreLastRootSkipsProjectCwd(t *testing.T) {
	last := t.TempDir()
	opts := seedRestore(t, last)
	for _, marker := range []string{".git", ".ike"} {
		cwd := t.TempDir()
		if err := os.Mkdir(filepath.Join(cwd, marker), 0o755); err != nil {
			t.Fatal(err)
		}
		if root, notice := RestoreLastRoot(opts, cwd); root != "" || notice != "" {
			t.Fatalf("%s cwd must suppress the restore, got %q/%q", marker, root, notice)
		}
	}
	// A plain directory still restores (#1000 behavior intact).
	if root, _ := RestoreLastRoot(opts, t.TempDir()); root == "" {
		t.Fatal("plain cwd must still restore the last project")
	}
}

func TestRestoreLastRootNeverTargetsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	opts := seedRestore(t, home)
	if root, notice := RestoreLastRoot(opts, t.TempDir()); root != "" || notice != "" {
		t.Fatalf("home at the history head must not restore, got %q/%q", root, notice)
	}
}

package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWatchPathDirectoryReportsEntries guards the dependency-marker case
// (#2613): a registered path that is a *directory* — the venv's site-packages,
// which lives below two pruned segments (`.venv` is dotted, `site-packages`
// is a vendor-noise name) — is watched as itself, and the entries created
// directly in it report. That is what an install's `<pkg>-<ver>.dist-info`
// looks like from the outside.
func TestWatchPathDirectoryReportsEntries(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, ".venv", "lib", "python3.12", "site-packages")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	if err := s.Start(root); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	// Unregistered, site-packages is pruned twice over and stays silent.
	if err := os.MkdirAll(filepath.Join(site, "before-1.0.dist-info"), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if c.count() != 0 {
		t.Fatalf("a pruned dependency tree must stay silent, got %v", c.msgs)
	}

	s.WatchPath(site)
	installed := filepath.Join(site, "rich-13.7.0.dist-info")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	found := false
	for _, ev := range got {
		if ev.Path == installed {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an event for %s, got %v", installed, got)
	}
}

// TestWatchPathDirectoryStaysNonRecursive guards the cost rule the whole
// design rests on (#2613): registering site-packages watches that one
// directory and nothing below it — neither the registration set nor the
// fsnotify watches descend into the package trees.
func TestWatchPathDirectoryStaysNonRecursive(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, ".venv", "lib", "python3.12", "site-packages")
	pkg := filepath.Join(site, "rich")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(pkg, "console.py")
	if err := os.WriteFile(deep, []byte("x = 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	if err := s.Start(root); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.WatchPath(site)

	if got := s.WatchedPaths(); len(got) != 1 || got[0] != site {
		t.Fatalf("WatchedPaths = %v, want exactly [%s]", got, site)
	}
	s.mu.Lock()
	dirs := make([]string, 0, len(s.extraDirs))
	for d := range s.extraDirs {
		dirs = append(dirs, d)
	}
	files := len(s.extraFiles)
	s.mu.Unlock()
	if len(dirs) != 1 || dirs[0] != site {
		t.Fatalf("fsnotify directory registrations = %v, want exactly [%s]", dirs, site)
	}
	if files != 0 {
		t.Fatalf("a marker directory must register no file watch, got %d", files)
	}

	// A write deep inside a package is not a marker change and must not report.
	if err := os.WriteFile(deep, []byte("x = 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	for _, ev := range c.all() {
		if ev.Path == deep {
			t.Fatalf("the marker directory's subtree must stay silent, got %v", ev)
		}
	}
}

// TestWatchPathDirectoryNeighboursStillFiltered keeps the #2506 guarantee
// intact: accepting a registered directory's entries must not also accept the
// neighbours of a *missing* registered file, whose parent directory carries
// the watch as a fallback.
func TestWatchPathDirectoryNeighboursStillFiltered(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	gone := filepath.Join(out, "gone.json")
	neighbour := filepath.Join(out, "neighbour.json")
	s, c := service()
	if err := s.Start(root); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.WatchPath(gone) // missing: the parent directory carries the watch

	if err := os.WriteFile(neighbour, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	for _, ev := range c.all() {
		if ev.Path == neighbour {
			t.Fatalf("a directory fallback must filter its neighbours, got %v", ev)
		}
	}
}

package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWatchPathOutsideRootReports guards #2506: a file the recursive root
// watch does not cover — the "diff two files" pane's /tmp side — reports its
// writes once it is registered by path.
func TestWatchPathOutsideRootReports(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "a.json")
	if err := os.WriteFile(outside, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	if err := s.Start(root); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	// Unregistered, the outside file is invisible.
	if err := os.WriteFile(outside, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if c.count() != 0 {
		t.Fatalf("an unwatched outside file must stay silent, got %v", c.msgs)
	}

	s.WatchPath(outside)
	if got := s.WatchedPaths(); len(got) != 1 || got[0] != outside {
		t.Fatalf("WatchedPaths = %v, want [%s]", got, outside)
	}
	if err := os.WriteFile(outside, []byte("three"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	found := false
	for _, ev := range got {
		if ev.Path == outside && (ev.Kind == FileChanged || ev.Kind == FileCreated) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a change for the registered %s, got %v", outside, got)
	}
}

// TestWatchPathNeighboursStaySilent guards the filter #2506 needs: while a
// registered file is missing its *directory* carries the watch, and a shared
// directory (/tmp) must not turn its neighbours' churn into events.
func TestWatchPathNeighboursStaySilent(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	gone := filepath.Join(out, "gone.json")
	neighbour := filepath.Join(out, "neighbour.json")

	s, c := service()
	if err := s.Start(root); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.WatchPath(gone) // never existed: the parent directory is watched instead

	if err := os.WriteFile(neighbour, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if c.count() != 0 {
		t.Fatalf("a neighbour of the registered path must stay silent, got %v", c.msgs)
	}

	// The registered file appearing does report.
	if err := os.WriteFile(gone, []byte("back"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	if got[0].Path != gone {
		t.Fatalf("expected the registered path, got %v", got)
	}
}

// TestUnwatchPathDropsRegistration guards the no-leak rule (#2506): the
// consumer's close pairs with UnwatchPath and the service forgets the path —
// exported state a consumer's test can assert — and reports nothing for it.
func TestUnwatchPathDropsRegistration(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "a.json")
	if err := os.WriteFile(outside, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	if err := s.Start(root); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	// Reference counted: two registrations need two releases.
	s.WatchPath(outside)
	s.WatchPath(outside)
	s.UnwatchPath(outside)
	if got := s.WatchedPaths(); len(got) != 1 {
		t.Fatalf("one release of two must keep the watch, got %v", got)
	}
	s.UnwatchPath(outside)
	if got := s.WatchedPaths(); len(got) != 0 {
		t.Fatalf("WatchedPaths = %v, want empty", got)
	}
	if err := os.WriteFile(outside, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if c.count() != 0 {
		t.Fatalf("a released path must stay silent, got %v", c.msgs)
	}
}

// TestWatchPathSurvivesRestart guards the project-switch case (#2506): Start
// rebuilds the fsnotify watcher, and the per-path registrations re-arm on it
// instead of silently going deaf.
func TestWatchPathSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "a.json")
	if err := os.WriteFile(outside, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, c := service()
	if err := s.Start(root); err != nil {
		t.Fatal(err)
	}
	s.WatchPath(outside)
	if err := s.Start(t.TempDir()); err != nil { // project switch
		t.Fatal(err)
	}
	defer s.Stop()
	if got := s.WatchedPaths(); len(got) != 1 || got[0] != outside {
		t.Fatalf("WatchedPaths after restart = %v, want [%s]", got, outside)
	}
	if err := os.WriteFile(outside, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.wait(t, 1)
	if got[0].Path != outside {
		t.Fatalf("expected the registered path after a restart, got %v", got)
	}
}

// TestWatchPathInsideRootNeedsNoSecondWatch guards the cheap case: a path the
// recursive walk already covers is remembered (so the consumer's bookkeeping
// stays uniform) without a second fsnotify registration.
func TestWatchPathInsideRootNeedsNoSecondWatch(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "a.json")
	if err := os.WriteFile(inside, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := service()
	if err := s.Start(root); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.WatchPath(inside)
	s.mu.Lock()
	files, dirs := len(s.extraFiles), len(s.extraDirs)
	s.mu.Unlock()
	if files != 0 || dirs != 0 {
		t.Fatalf("in-root path registered %d file / %d dir watches, want none", files, dirs)
	}
	if got := s.WatchedPaths(); len(got) != 1 {
		t.Fatalf("WatchedPaths = %v, want the path recorded", got)
	}
}

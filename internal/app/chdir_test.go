package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chdir is t.Chdir for this package (#3054). The render hot path reads the
// working directory through cachedGetwd (cwdcache.go), which is refreshed only
// on a project switch — so t.Chdir's cleanup, which changes the directory
// back, would leave the cache pointing at a temp directory that no longer
// exists, and the tests that run next spawn shells and resolve fixtures under
// it. The cache is dropped after the restore (the cleanup is registered first,
// so it runs last). Within the test the cache is left alone: a fixture that
// wants the cache to follow its chdir calls invalidateCwd itself, and one
// (extractProjectRoot) deliberately keeps it pointing at the project root.
func chdir(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(invalidateCwd)
	t.Chdir(dir)
}

// TestChdirRestoresTheCwdCache: after a test that used chdir, the cache
// reports the restored directory, not the one the test visited.
func TestChdirRestoresTheCwdCache(t *testing.T) {
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	visited := t.TempDir()
	t.Run("visit", func(t *testing.T) {
		chdir(t, visited)
		invalidateCwd()
		got, err := cachedGetwd()
		if err != nil {
			t.Fatal(err)
		}
		if !sameDir(t, got, visited) {
			t.Fatalf("cachedGetwd after chdir = %q, want %q", got, visited)
		}
	})
	got, err := cachedGetwd()
	if err != nil {
		t.Fatal(err)
	}
	if !sameDir(t, got, before) {
		t.Fatalf("cachedGetwd after the subtest = %q, want the restored %q", got, before)
	}
}

// TestNoRawChdirInTests is the sweep guard: every directory change in this
// package's tests goes through chdir, so the cwd cache never outlives a test.
func TestNoRawChdirInTests(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	// cwdcache_test.go exercises the cache against raw os.Chdir on purpose, and
	// cwdguard_test.go's hook is what puts a vanished directory back.
	allowed := map[string]bool{"chdir_test.go": true, "cwdcache_test.go": true, "cwdguard_test.go": true}
	for _, f := range files {
		if allowed[f] {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, ".Chdir(") {
				t.Errorf("%s:%d: use chdir(t, dir) instead of t.Chdir — it keeps the cwd cache honest", f, i+1)
			}
		}
	}
}

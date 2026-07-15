package app

import (
	"os"
	"testing"
)

// TestCachedGetwdCachesUntilInvalidated verifies the working directory is read
// once and reused, and that invalidateCwd forces a fresh read after a chdir
// (#608). Correctness matters: a stale cwd would mis-shorten paths in the status
// line and title.
func TestCachedGetwdCachesUntilInvalidated(t *testing.T) {
	invalidateCwd()
	start, err := cachedGetwd()
	if err != nil {
		t.Fatal(err)
	}
	real, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if start != real {
		t.Fatalf("cachedGetwd = %q, want %q", start, real)
	}

	// Change directory without invalidating: the cache must still report the old
	// value (proving it does not hit the OS every call).
	tmp := t.TempDir()
	realTmp, _ := os.Getwd() // baseline before chdir for restore
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(realTmp)
	if got, _ := cachedGetwd(); got != start {
		t.Fatalf("cachedGetwd changed without invalidate: %q (want cached %q)", got, start)
	}

	// After invalidation the next read reflects the new directory.
	invalidateCwd()
	got, err := cachedGetwd()
	if err != nil {
		t.Fatal(err)
	}
	// macOS temp dirs are symlinked (/var -> /private/var); compare against the
	// same resolution os.Getwd performs.
	want, _ := os.Getwd()
	if got != want {
		t.Fatalf("after invalidate cachedGetwd = %q, want %q", got, want)
	}

	invalidateCwd() // leave the cache clean for other tests
}

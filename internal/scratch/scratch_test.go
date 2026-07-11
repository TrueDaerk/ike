package scratch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// sandbox points the store at a fresh IKE_CONFIG_DIR and returns the expected
// scratch dir.
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	return filepath.Join(dir, "scratches")
}

func TestDirHonorsConfigDirOverride(t *testing.T) {
	want := sandbox(t)
	got, err := Dir()
	if err != nil || got != want {
		t.Fatalf("Dir() = %q, %v; want %q", got, err, want)
	}
}

func TestCreateAllocatesSequentially(t *testing.T) {
	dir := sandbox(t)

	first, err := Create("py")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "scratch-1.py"); first != want {
		t.Fatalf("first = %q, want %q", first, want)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("scratch must exist on disk: %v", err)
	}
	// The counter skips existing names, dot-optional extension.
	second, err := Create(".py")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "scratch-2.py"); second != want {
		t.Fatalf("second = %q, want %q", second, want)
	}
	// A different extension restarts at the first free N for that name.
	other, err := Create("")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "scratch-1.txt"); other != want {
		t.Fatalf("empty ext must mean txt: %q, want %q", other, want)
	}
}

func TestListNewestFirstAndMissingDir(t *testing.T) {
	sandbox(t)

	// Missing dir: empty list, no error.
	if got, err := List(); err != nil || len(got) != 0 {
		t.Fatalf("List() on missing dir = %v, %v", got, err)
	}

	a, _ := Create("txt")
	b, _ := Create("txt")
	// Make the first strictly older so mod-time ordering is deterministic.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(a, old, old); err != nil {
		t.Fatal(err)
	}
	got, err := List()
	if err != nil || len(got) != 2 {
		t.Fatalf("List() = %v, %v", got, err)
	}
	if got[0] != b || got[1] != a {
		t.Fatalf("want newest first [%q %q], got %v", b, a, got)
	}
}

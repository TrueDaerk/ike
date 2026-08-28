package scratch

import (
	"os"
	"path/filepath"
	"testing"
)

// setext_test.go covers the metadata and the language change the scratch
// manager (#2256) is built on.

// TestEntriesCarrySize pins the metadata the manager lists: Entries reports
// each file's byte size next to its mod time.
func TestEntriesCarrySize(t *testing.T) {
	sandbox(t)
	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Size != 10 {
		t.Fatalf("size = %d, want 10", entries[0].Size)
	}
}

// TestSetExtSwapsExtension covers the language change: the stem survives, only
// the extension changes, and the old name is gone.
func TestSetExtSwapsExtension(t *testing.T) {
	dir := sandbox(t)
	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := SetExt(path, ".py")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "scratch-1.py"); got != want {
		t.Fatalf("SetExt = %q, want %q", got, want)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the old extension must be gone")
	}
	// An empty extension means txt, exactly like Create's.
	back, err := SetExt(got, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "scratch-1.txt"); back != want {
		t.Fatalf(`SetExt("") = %q, want %q`, back, want)
	}
}

// TestSetExtRefusesCollisionAndOutsiders pins the guards it inherits from
// Rename: an existing target is never overwritten, and a path outside the
// store is refused.
func TestSetExtRefusesCollisionAndOutsiders(t *testing.T) {
	sandbox(t)
	txt, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	other, err := Create("py")
	if err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(filepath.Dir(other), "scratch-1.md")
	if err := os.Rename(other, md); err != nil {
		t.Fatal(err)
	}
	if _, err := SetExt(md, "txt"); err == nil {
		t.Fatal("SetExt must refuse an existing target")
	}
	if _, err := os.Stat(txt); err != nil {
		t.Fatalf("the existing scratch must be untouched: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(outside, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SetExt(outside, "py"); err == nil {
		t.Fatal("SetExt must refuse a path outside the store")
	}
}

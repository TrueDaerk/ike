package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromoteMovesScratchOutOfStore is #2339's store-level headline: the file
// lands at the target path with its content and the store entry is gone.
func TestPromoteMovesScratchOutOfStore(t *testing.T) {
	sandbox(t)
	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "nested", "notes.txt")

	if err := Promote(path, target); err != nil {
		t.Fatalf("Promote = %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("promoted file must exist: %v", err)
	}
	if string(data) != "keep me\n" {
		t.Fatalf("promoted content = %q, want the scratch content", data)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the scratch must be gone from the store, stat err = %v", err)
	}
	if got, err := List(); err != nil || len(got) != 0 {
		t.Fatalf("List() after promote = %v, %v", got, err)
	}
}

// TestPromoteRefusesExistingTarget is the no-clobber criterion: an occupied
// path is refused and the scratch stays in the store, so the user can pick
// another name without having lost anything.
func TestPromoteRefusesExistingTarget(t *testing.T) {
	sandbox(t)
	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "taken.txt")
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = Promote(path, target)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Promote onto an existing path = %v, want an already-exists error", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "original\n" {
		t.Fatalf("the existing file must be untouched, got %q", data)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the refused scratch must survive: %v", err)
	}
}

// TestPromoteRefusesBadSourceAndTarget covers the guard rails: the source must
// be a file directly inside the store and the target must lie outside it.
func TestPromoteRefusesBadSourceAndTarget(t *testing.T) {
	dir := sandbox(t)
	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A source outside the store is not this package's to move.
	if err := Promote(outside, filepath.Join(t.TempDir(), "moved.txt")); err == nil {
		t.Fatal("promoting a file outside the store must be refused")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("the file outside the store must survive: %v", err)
	}
	// A target inside the store would be a rename, not a promotion.
	if err := Promote(path, filepath.Join(dir, "other.txt")); err == nil {
		t.Fatal("promoting into the store must be refused")
	}
	// An empty target names nothing.
	if err := Promote(path, ""); err == nil {
		t.Fatal("an empty target must be refused")
	}
	// A vanished source fails rather than creating an empty target.
	gone := filepath.Join(dir, "scratch-9.txt")
	if err := Promote(gone, filepath.Join(t.TempDir(), "x.txt")); err == nil {
		t.Fatal("promoting a missing scratch must fail")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the untouched scratch must still be there: %v", err)
	}
}

// TestPromoteWriteErrorKeepsScratch is the "no half-removed store entry"
// criterion: when the target cannot be created the scratch stays put.
func TestPromoteWriteErrorKeepsScratch(t *testing.T) {
	sandbox(t)
	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	// A regular file where a parent directory would have to be.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Promote(path, filepath.Join(blocker, "child.txt")); err == nil {
		t.Fatal("promoting under a non-directory must fail")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the scratch must survive a failed promote: %v", err)
	}
}

// TestIsScratchBoundary covers the predicate the promote command gates on.
func TestIsScratchBoundary(t *testing.T) {
	dir := sandbox(t)
	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	if !IsScratch(path) {
		t.Fatalf("%q must count as a scratch", path)
	}
	// A name that does not exist yet still lies in the store — the predicate
	// is about location, and Promote does the existence check itself.
	if !IsScratch(filepath.Join(dir, "nope.txt")) {
		t.Fatal("a store-local path must count as a scratch")
	}
	for _, p := range []string{"", filepath.Join(t.TempDir(), "elsewhere.txt"), filepath.Join(dir, "sub", "deep.txt")} {
		if IsScratch(p) {
			t.Fatalf("%q must not count as a scratch", p)
		}
	}
}

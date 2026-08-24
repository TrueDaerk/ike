package langgo

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lang"
)

// coverage_test.go covers the Go cover-profile parser (#2081): block-to-line
// mapping, the covered/uncovered/partial classification, and the resolution
// of import-qualified profile paths against the module root.

// writeCoverFixture builds a module root with a package dir and a profile
// referencing it by import path; returns the profile path, the run dir (the
// package dir, like a real `go test` run) and the source file's disk path.
func writeCoverFixture(t *testing.T, profileBody string) (profile, dir, src string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/mod\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src = filepath.Join(dir, "f.go")
	profile = filepath.Join(t.TempDir(), "cover.out")
	if err := os.WriteFile(profile, []byte(profileBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return profile, dir, src
}

// TestParseCoverProfileLines: executed blocks mark lines covered, unexecuted
// ones uncovered, an overlap partial; the mode header is skipped.
func TestParseCoverProfileLines(t *testing.T) {
	profile, dir, src := writeCoverFixture(t, ""+
		"mode: set\n"+
		"example.com/mod/pkg/f.go:3.10,5.2 2 1\n"+
		"example.com/mod/pkg/f.go:7.2,8.16 1 0\n"+
		"example.com/mod/pkg/f.go:8.20,10.2 1 1\n")
	files, err := parseCoverProfile(profile, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != src {
		t.Fatalf("files = %+v, want one entry for %s", files, src)
	}
	want := map[int]lang.CoverKind{
		3: lang.CoverCovered, 4: lang.CoverCovered, 5: lang.CoverCovered,
		7: lang.CoverUncovered,
		8: lang.CoverPartial, // in a count-0 and a count-1 block
		9: lang.CoverCovered, 10: lang.CoverCovered,
	}
	if len(files[0].Lines) != len(want) {
		t.Fatalf("lines = %v, want %v", files[0].Lines, want)
	}
	for l, k := range want {
		if files[0].Lines[l] != k {
			t.Fatalf("line %d = %v, want %v", l, files[0].Lines[l], k)
		}
	}
}

// TestParseCoverProfileSkipsForeignModules: an entry outside the run's module
// cannot resolve to a file and is dropped instead of inventing a path.
func TestParseCoverProfileSkipsForeignModules(t *testing.T) {
	profile, dir, _ := writeCoverFixture(t, ""+
		"mode: set\n"+
		"other.org/dep/x.go:1.1,2.2 1 1\n")
	files, err := parseCoverProfile(profile, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("foreign-module entries must be skipped, got %+v", files)
	}
}

// TestParseCoverProfileMissingProfile: a vanished temp file is an error, not
// a panic or empty success.
func TestParseCoverProfileMissingProfile(t *testing.T) {
	if _, err := parseCoverProfile(filepath.Join(t.TempDir(), "gone.out"), t.TempDir()); err == nil {
		t.Fatal("a missing profile must error")
	}
}

// TestModuleAtWalksUp: the module declaration resolves from a nested dir.
func TestModuleAtWalksUp(t *testing.T) {
	_, dir, _ := writeCoverFixture(t, "mode: set\n")
	modPath, modRoot := moduleAt(dir)
	if modPath != "example.com/mod" || modRoot != filepath.Dir(dir) {
		t.Fatalf("moduleAt = %q %q", modPath, modRoot)
	}
	if p, r := moduleAt(t.TempDir()); p != "" || r != "" {
		t.Fatalf("no go.mod must yield empty results, got %q %q", p, r)
	}
}

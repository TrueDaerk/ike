package jqplay

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// library_test.go covers the named saved-filter store (#1995): persistence
// across a load/save round trip (the "survives a restart" acceptance case),
// the add/rename/delete edits and the tolerance a state file on disk needs.

// libPath is a throwaway store path inside a nested directory, so Save also
// has to create the directory the way the project's .ike does on first use.
func libPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state", "jqfilters.json")
}

func TestLibraryRoundTripSurvivesReload(t *testing.T) {
	path := libPath(t)
	lib := LoadLibrary(path)
	if lib.Len() != 0 {
		t.Fatalf("a missing store must load empty, got %d", lib.Len())
	}
	if err := lib.Set("es hits", ".hits.hits[] | ._source"); err != nil {
		t.Fatal(err)
	}
	if err := lib.Set("errors", `.[] | select(.level == "error")`); err != nil {
		t.Fatal(err)
	}
	if err := lib.Save(path); err != nil {
		t.Fatal(err)
	}

	// A fresh process reads the same two filters back.
	again := LoadLibrary(path)
	if again.Len() != 2 {
		t.Fatalf("got %d filters after reload, want 2", again.Len())
	}
	f, ok := again.Get("es hits")
	if !ok || f.Program != ".hits.hits[] | ._source" {
		t.Fatalf("es hits came back as %+v (ok=%v)", f, ok)
	}
	// Sorted by name, case-insensitively: "errors" before "es hits".
	if again.All()[0].Name != "errors" {
		t.Fatalf("store order is %v, want errors first", again.All())
	}
}

func TestLibrarySetOverwritesSameName(t *testing.T) {
	lib := &Library{}
	if err := lib.Set("f", ".a"); err != nil {
		t.Fatal(err)
	}
	if err := lib.Set("  f  ", ".b"); err != nil {
		t.Fatal(err)
	}
	if lib.Len() != 1 {
		t.Fatalf("a repeat name must overwrite, got %d entries", lib.Len())
	}
	if f, _ := lib.Get("f"); f.Program != ".b" {
		t.Fatalf("program is %q, want .b", f.Program)
	}
}

func TestLibrarySetRejectsEmpty(t *testing.T) {
	lib := &Library{}
	if err := lib.Set("  ", ".a"); !errors.Is(err, ErrNoName) {
		t.Fatalf("empty name gave %v, want ErrNoName", err)
	}
	if err := lib.Set("f", "   "); !errors.Is(err, ErrNoProgram) {
		t.Fatalf("empty program gave %v, want ErrNoProgram", err)
	}
	if lib.Len() != 0 {
		t.Fatalf("a rejected save must store nothing, got %d", lib.Len())
	}
}

func TestLibraryRenameAndDelete(t *testing.T) {
	lib := &Library{}
	_ = lib.Set("old", ".a")
	_ = lib.Set("other", ".b")

	if err := lib.Rename("old", "new"); err != nil {
		t.Fatal(err)
	}
	if lib.Has("old") {
		t.Fatal("the old name must be gone after a rename")
	}
	if f, ok := lib.Get("new"); !ok || f.Program != ".a" {
		t.Fatalf("rename lost the program: %+v ok=%v", f, ok)
	}
	if err := lib.Rename("new", "other"); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("renaming onto a taken name gave %v, want ErrNameTaken", err)
	}
	if err := lib.Rename("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("renaming an unknown filter gave %v, want ErrNotFound", err)
	}

	if err := lib.Delete("new"); err != nil {
		t.Fatal(err)
	}
	if lib.Len() != 1 || lib.All()[0].Name != "other" {
		t.Fatalf("delete left %v", lib.All())
	}
	if err := lib.Delete("new"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting twice gave %v, want ErrNotFound", err)
	}
}

func TestLibraryDeletePersists(t *testing.T) {
	path := libPath(t)
	lib := &Library{}
	_ = lib.Set("a", ".a")
	_ = lib.Set("b", ".b")
	if err := lib.Save(path); err != nil {
		t.Fatal(err)
	}
	_ = lib.Delete("a")
	if err := lib.Save(path); err != nil {
		t.Fatal(err)
	}
	if again := LoadLibrary(path); again.Len() != 1 || again.Has("a") {
		t.Fatalf("a deleted filter came back: %v", again.All())
	}
}

func TestLibraryLoadToleratesBrokenStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jqfilters.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if lib := LoadLibrary(path); lib == nil || lib.Len() != 0 {
		t.Fatal("a malformed store must load as an empty library")
	}
	// A hand-edited file with junk entries: they are dropped, the good one stays.
	body := `{"filters":[{"name":"","program":".a"},{"name":"x","program":"  "},
	           {"name":" keep ","program":" .b "},{"name":"keep","program":".c"}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := LoadLibrary(path)
	if lib.Len() != 1 {
		t.Fatalf("got %v, want only the one usable entry", lib.All())
	}
	if f, _ := lib.Get("keep"); f.Program != ".b" {
		t.Fatalf("entry came back as %q, want the trimmed .b", f.Program)
	}
}

func TestLibraryFullRefusesNewNames(t *testing.T) {
	lib := &Library{}
	for i := 0; i < MaxFilters; i++ {
		if err := lib.Set(strings.Repeat("f", 1)+string(rune('a'+i%26))+strings.Repeat("x", i), ".a"); err != nil {
			t.Fatalf("filling the library failed at %d: %v", i, err)
		}
	}
	if err := lib.Set("one more", ".a"); !errors.Is(err, ErrLibraryFull) {
		t.Fatalf("a full library gave %v, want ErrLibraryFull", err)
	}
	// Overwriting an existing name still works at the cap.
	if err := lib.Set(lib.All()[0].Name, ".z"); err != nil {
		t.Fatalf("overwrite at the cap failed: %v", err)
	}
}

func TestPreviewCollapsesAndTruncates(t *testing.T) {
	if got := Preview(".hits\n | .hits[]\t| ._source", 0); got != ".hits | .hits[] | ._source" {
		t.Fatalf("preview collapsed to %q", got)
	}
	if got := Preview(".abcdefgh", 5); got != ".abc…" {
		t.Fatalf("preview truncated to %q, want .abc…", got)
	}
	if got := Preview(".abc", 10); got != ".abc" {
		t.Fatalf("a short program must be left alone, got %q", got)
	}
}

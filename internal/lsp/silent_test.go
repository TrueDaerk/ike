package lsp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// silent_test.go covers the known-silent server store (#3080): marks are per
// project and per server id, survive a save/load round trip, clear one id or
// all, and an emptied store removes its file.

func TestSilentStoreRoundTrip(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	a, b := t.TempDir(), t.TempDir()
	path := SilentPath(a)
	if path != SilentPath(b) || filepath.Base(path) != SilentFileName {
		t.Fatalf("IKE_CONFIG_DIR path = %q / %q, want one shared %s", path, SilentPath(b), SilentFileName)
	}
	s, err := LoadSilent(path)
	if err != nil || len(s.Projects) != 0 {
		t.Fatalf("missing file = %v, %v; want an empty store", s, err)
	}
	if !s.Mark(a, "go") || !s.Mark(a, "python") || s.Mark(a, "go") || !s.Mark(b, "go") {
		t.Fatal("Mark must report a new id once")
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSilent(path)
	if err != nil {
		t.Fatal(err)
	}
	if ids := got.IDs(a); !reflect.DeepEqual(ids, []string{"go", "python"}) {
		t.Errorf("IDs(a) = %v", ids)
	}
	if !got.Has(b, "go") || got.Has(b, "python") {
		t.Errorf("project b = %v, want only go", got.IDs(b))
	}
	if !got.Clear(a, "go") || got.Clear(a, "go") || got.Has(a, "go") || !got.Has(a, "python") {
		t.Errorf("Clear(a, go) left %v", got.IDs(a))
	}
	if !got.Clear(b, "") || len(got.IDs(b)) != 0 {
		t.Errorf("Clear(b, \"\") left %v", got.IDs(b))
	}
	got.Clear(a, "")
	if err := got.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("an emptied store must remove its file, stat = %v", err)
	}
}

// TestSilentStoreKeyNormalised: a relative root, a trailing slash and the
// absolute path name the same project.
func TestSilentStoreKeyNormalised(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	s := &SilentStore{}
	s.Mark(".", "go")
	if !s.Has(root+string(filepath.Separator), "go") {
		t.Errorf("relative and absolute roots must match, store = %v", s.Projects)
	}
}

func TestSilentStoreMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), SilentFileName)
	for _, body := range []string{"{", `{"version":9,"projects":{}}`} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := LoadSilent(path)
		if err == nil || len(s.Projects) != 0 {
			t.Errorf("%q: got %v, %v; want an error and an empty store", body, s, err)
		}
	}
	// UpdateSilent replaces a malformed file with the change.
	s, err := UpdateSilent(path, func(s *SilentStore) bool { return s.Mark("/p", "go") })
	if err != nil || !s.Has("/p", "go") {
		t.Fatalf("UpdateSilent = %v, %v", s, err)
	}
	if again, _ := LoadSilent(path); !again.Has("/p", "go") {
		t.Error("UpdateSilent did not persist")
	}
}

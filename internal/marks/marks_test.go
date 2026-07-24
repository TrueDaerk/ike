package marks

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRoundTrip: marks set in one store are read back by a fresh store over
// the same state directory (the restart case), path canonicalized to abs.
func TestRoundTrip(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	s := &Store{}
	s.Set('A', "/tmp/a.go", 4, 2)
	s.Set('B', "/tmp/b.go", 0, 0)

	fresh := &Store{}
	mk, ok := fresh.Get('A')
	if !ok || mk.Path != "/tmp/a.go" || mk.Line != 4 || mk.Col != 2 {
		t.Fatalf("Get(A) = %+v %v, want /tmp/a.go:4:2", mk, ok)
	}
	if all := fresh.All(); len(all) != 2 || all[0].Letter != 'A' || all[1].Letter != 'B' {
		t.Fatalf("All = %+v, want A then B", all)
	}
}

// TestRemovePersists: removal survives a reload; removing the last mark
// deletes the store file.
func TestRemovePersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	s := &Store{}
	s.Set('A', "/tmp/a.go", 1, 0)
	s.Set('B', "/tmp/b.go", 2, 0)
	s.Remove('A')

	fresh := &Store{}
	if _, ok := fresh.Get('A'); ok {
		t.Fatal("removed mark came back after reload")
	}
	s.Remove('B')
	if _, err := os.Stat(filepath.Join(dir, "marks.json")); !os.IsNotExist(err) {
		t.Fatal("empty store must remove its file")
	}
}

// TestOnlyGlobalLetters: lowercase letters and empty paths are rejected.
func TestOnlyGlobalLetters(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	s := &Store{}
	s.Set('a', "/tmp/a.go", 1, 0)
	s.Set('A', "", 1, 0)
	if all := s.All(); len(all) != 0 {
		t.Fatalf("All = %+v, want empty", all)
	}
}

// TestLines reports the marked lines of one path only, sorted.
func TestLines(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	s := &Store{}
	s.Set('B', "/tmp/a.go", 9, 0)
	s.Set('A', "/tmp/a.go", 3, 0)
	s.Set('C', "/tmp/b.go", 1, 0)
	got := s.Lines("/tmp/a.go")
	if len(got) != 2 || got[0] != 3 || got[1] != 9 {
		t.Fatalf("Lines = %v, want [3 9]", got)
	}
}

// TestAdjustEditShifts mirrors the breakpoint store semantics: inserts push
// marks below the edit down, deletes pull them up (clamped at the cursor),
// and the shifted set persists.
func TestAdjustEditShifts(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	s := &Store{}
	s.Set('A', "/tmp/a.go", 5, 1)
	s.Set('B', "/tmp/a.go", 1, 0)
	s.Set('C', "/tmp/b.go", 5, 0)

	// Insert one line: cursor lands on line 3, delta +1 → A (5) shifts to 6,
	// B (1) stays, the other file's C is untouched.
	s.AdjustEdit("/tmp/a.go", 3, 1)
	if mk, _ := s.Get('A'); mk.Line != 6 {
		t.Fatalf("A after insert = %d, want 6", mk.Line)
	}
	if mk, _ := s.Get('B'); mk.Line != 1 {
		t.Fatalf("B after insert = %d, want 1", mk.Line)
	}
	if mk, _ := s.Get('C'); mk.Line != 5 {
		t.Fatalf("C (other file) = %d, want 5", mk.Line)
	}

	// Delete three lines at cursor 2: A (6) pulls up to 3.
	s.AdjustEdit("/tmp/a.go", 2, -3)
	if mk, _ := s.Get('A'); mk.Line != 3 {
		t.Fatalf("A after delete = %d, want 3", mk.Line)
	}

	// The adjustment persisted.
	fresh := &Store{}
	if mk, _ := fresh.Get('A'); mk.Line != 3 {
		t.Fatalf("A after reload = %d, want 3", mk.Line)
	}
}

// TestMalformedFileReadsEmpty: a corrupt store file degrades to no marks.
func TestMalformedFileReadsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "marks.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Store{}
	if all := s.All(); len(all) != 0 {
		t.Fatalf("All = %+v, want empty on malformed file", all)
	}
}

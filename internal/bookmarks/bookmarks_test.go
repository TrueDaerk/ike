package bookmarks

import (
	"os"
	"path/filepath"
	"testing"
)

// bookmarks_test.go covers the project bookmark store (#55): toggling,
// mnemonic uniqueness, notes, the edit shift, the rename hook, gutter signs
// and the persistence round trip.

// TestToggleAddsAndRemoves: toggling flips one line's bookmark.
func TestToggleAddsAndRemoves(t *testing.T) {
	s := New()
	if !s.Toggle("a.go", 4) || !s.Has("a.go", 4) {
		t.Fatal("first toggle must set the bookmark")
	}
	if s.Toggle("a.go", 4) || s.Has("a.go", 4) {
		t.Fatal("second toggle must remove it")
	}
	if s.Count() != 0 {
		t.Fatalf("count = %d, want 0", s.Count())
	}
}

// TestMnemonicIsUniqueAcrossProject: assigning a digit again moves it, the
// previous holder staying as an anonymous bookmark.
func TestMnemonicIsUniqueAcrossProject(t *testing.T) {
	s := New()
	s.SetMnemonic("a.go", 1, '3')
	s.SetMnemonic("b.go", 7, '3')

	b, ok := s.ByMnemonic('3')
	if !ok || b.Path != "b.go" || b.Line != 7 {
		t.Fatalf("mnemonic 3 = %+v %v, want b.go:7", b, ok)
	}
	old, ok := s.At("a.go", 1)
	if !ok || !old.Anonymous() {
		t.Fatalf("previous holder = %+v %v, want an anonymous bookmark", old, ok)
	}
}

// TestSetNoteCreatesAndClears: annotating a bare line creates the bookmark,
// an empty note only drops the annotation.
func TestSetNoteCreatesAndClears(t *testing.T) {
	s := New()
	s.SetNote("a.go", 2, "  fix this  ")
	b, ok := s.At("a.go", 2)
	if !ok || b.Note != "fix this" {
		t.Fatalf("annotated bookmark = %+v %v", b, ok)
	}
	s.SetNote("a.go", 2, "")
	b, ok = s.At("a.go", 2)
	if !ok || b.Note != "" {
		t.Fatalf("cleared bookmark = %+v %v, want a note-less bookmark", b, ok)
	}
}

// TestSignsRendersMnemonicOverFlag: the gutter source shows the digit for a
// mnemonic bookmark and the flag for an anonymous one.
func TestSignsRendersMnemonicOverFlag(t *testing.T) {
	s := New()
	s.Add("a.go", 0)
	s.SetMnemonic("a.go", 5, '7')
	signs := s.Signs("a.go")
	if signs[0] != "⚑" || signs[5] != "7" {
		t.Fatalf("signs = %+v, want 0:⚑ 5:7", signs)
	}
	if s.Signs("other.go") != nil {
		t.Fatal("a file without bookmarks must report no signs")
	}
}

// TestAdjustEditShiftsBelowTheEdit: an insertion pushes the bookmarks below
// it down, a deletion pulls them up — the breakpoint store's semantics.
func TestAdjustEditShiftsBelowTheEdit(t *testing.T) {
	s := New()
	s.Add("a.go", 2)
	s.SetMnemonic("a.go", 10, '1')

	s.AdjustEdit("a.go", 5, 3) // three lines inserted with the cursor on 5
	if !s.Has("a.go", 2) {
		t.Fatal("a bookmark above the edit must not move")
	}
	b, ok := s.At("a.go", 13)
	if !ok || b.Mnemonic != '1' {
		t.Fatalf("shifted bookmark = %+v %v, want the mnemonic on line 13", b, ok)
	}

	s.AdjustEdit("a.go", 5, -3)
	if _, ok := s.At("a.go", 10); !ok {
		t.Fatal("the deletion must pull the bookmark back up to line 10")
	}
}

// TestAdjustEditMergesCollisions: two bookmarks squeezed onto one line by a
// deletion collapse into a single entry.
func TestAdjustEditMergesCollisions(t *testing.T) {
	s := New()
	s.SetNote("a.go", 4, "keeper")
	s.SetNote("a.go", 6, "later")
	s.AdjustEdit("a.go", 3, -4)
	if n := s.Count(); n != 1 {
		t.Fatalf("count after the merge = %d, want 1", n)
	}
	b := s.All()[0]
	if b.Line != 3 || b.Note != "keeper" {
		t.Fatalf("merged bookmark = %+v, want line 3 with the lower line's note", b)
	}
}

// TestRenameFollowsFilesAndDirectories: the explorer hook re-keys a renamed
// file and everything below a renamed directory.
func TestRenameFollowsFilesAndDirectories(t *testing.T) {
	s := New()
	s.Add("old.go", 1)
	s.Add(filepath.Join("pkg", "deep", "x.go"), 2)

	s.Rename("old.go", "new.go")
	s.Rename("pkg", "lib")
	if !s.Has("new.go", 1) || s.Has("old.go", 1) {
		t.Fatalf("file rename left %+v", s.All())
	}
	if !s.Has(filepath.Join("lib", "deep", "x.go"), 2) {
		t.Fatalf("directory rename left %+v", s.All())
	}
	for _, b := range s.All() {
		if _, ok := s.At(b.Path, b.Line); !ok {
			t.Fatalf("bookmark %+v lost its path field", b)
		}
	}
}

// TestPersistenceRoundTrip: a saved store reloads with mnemonics and notes,
// and an emptied one removes its file.
func TestPersistenceRoundTrip(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	s := New()
	s.Add("a.go", 3)
	s.SetMnemonic("b.go", 8, '2')
	s.SetNote("b.go", 8, "entry point")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	back := Load()
	if !back.Has("a.go", 3) {
		t.Fatalf("reloaded store = %+v", back.All())
	}
	b, ok := back.ByMnemonic('2')
	if !ok || b.Path != "b.go" || b.Line != 8 || b.Note != "entry point" {
		t.Fatalf("reloaded mnemonic bookmark = %+v %v", b, ok)
	}

	back.Remove("a.go", 3)
	back.Remove("b.go", 8)
	if err := back.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(File()); !os.IsNotExist(err) {
		t.Fatalf("an empty store must remove its file, stat err = %v", err)
	}
}

// TestLoadMalformedIsEmpty: bookmarks are convenience state — a broken file
// never fails a start.
func TestLoadMalformedIsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "bookmarks.json"), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := Load().Count(); n != 0 {
		t.Fatalf("malformed store loaded %d bookmarks", n)
	}
}

// TestLoadDedupesMnemonics: a hand-edited file repeating a digit keeps one
// holder, the rest staying anonymous.
func TestLoadDedupesMnemonics(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	data := `{"version":1,"files":{"a.go":[{"line":1,"mnemonic":"5"}],"b.go":[{"line":2,"mnemonic":"5"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "bookmarks.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Load()
	if s.Count() != 2 {
		t.Fatalf("count = %d, want both bookmarks kept", s.Count())
	}
	b, ok := s.ByMnemonic('5')
	if !ok || b.Path != "a.go" {
		t.Fatalf("mnemonic 5 = %+v %v, want the first bookmark", b, ok)
	}
	other, _ := s.At("b.go", 2)
	if !other.Anonymous() {
		t.Fatalf("duplicate holder = %+v, want it anonymous", other)
	}
}

// TestAllOrdersByPathThenLine: the picker's and stepper's canonical order.
func TestAllOrdersByPathThenLine(t *testing.T) {
	s := New()
	s.Add("b.go", 1)
	s.Add("a.go", 9)
	s.Add("a.go", 2)
	got := s.All()
	want := []struct {
		path string
		line int
	}{{"a.go", 2}, {"a.go", 9}, {"b.go", 1}}
	for i, w := range want {
		if got[i].Path != w.path || got[i].Line != w.line {
			t.Fatalf("All()[%d] = %+v, want %s:%d", i, got[i], w.path, w.line)
		}
	}
}

// TestPersistenceKeepsAnonymousDescriptions guards #2251: the overview shows
// descriptions on anonymous bookmarks too, so a note without a mnemonic must
// survive the round trip.
func TestPersistenceKeepsAnonymousDescriptions(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	s := New()
	s.SetNote("a.go", 3, "why this line")
	s.SetNote("a.go", 9, "and this one")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	back := Load()
	first, ok := back.At("a.go", 3)
	if !ok || first.Note != "why this line" || !first.Anonymous() {
		t.Fatalf("reloaded a.go:3 = %+v %v", first, ok)
	}
	second, ok := back.At("a.go", 9)
	if !ok || second.Note != "and this one" {
		t.Fatalf("reloaded a.go:9 = %+v %v", second, ok)
	}
}

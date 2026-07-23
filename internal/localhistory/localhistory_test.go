package localhistory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRecordListReadRoundTrip: recorded snapshots list newest-first and read
// back byte-identically, across store instances (persistence).
func TestRecordListReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	tick := 0
	s.now = func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Minute) }

	path := filepath.Join(t.TempDir(), "f.txt")
	s.Record(path, []byte("v1\n"))
	s.Record(path, []byte("v2\n"))

	// A fresh store over the same dir sees the same history.
	s2 := New(dir)
	entries := s2.List(path)
	if len(entries) != 2 {
		t.Fatalf("List = %d entries, want 2", len(entries))
	}
	if !entries[0].Time.After(entries[1].Time) {
		t.Fatalf("List not newest-first: %v then %v", entries[0].Time, entries[1].Time)
	}
	got, err := s2.Read(entries[0].Hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "v2\n" {
		t.Fatalf("newest snapshot = %q, want %q", got, "v2\n")
	}
	if got, _ := s2.Read(entries[1].Hash); string(got) != "v1\n" {
		t.Fatalf("older snapshot = %q, want %q", got, "v1\n")
	}
}

// TestDedupeConsecutiveSaves: saving identical content twice in a row stores
// one entry and one object.
func TestDedupeConsecutiveSaves(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	path := "/tmp/dedupe.txt"
	s.Record(path, []byte("same\n"))
	s.Record(path, []byte("same\n"))
	if n := len(s.List(path)); n != 1 {
		t.Fatalf("List = %d entries after identical saves, want 1", n)
	}
	objs, err := os.ReadDir(filepath.Join(dir, "objects"))
	if err != nil {
		t.Fatalf("objects dir: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("objects = %d, want 1", len(objs))
	}
	// A different save after the pair still records.
	s.Record(path, []byte("other\n"))
	if n := len(s.List(path)); n != 2 {
		t.Fatalf("List = %d entries after a distinct save, want 2", n)
	}
}

// TestPruneCount: the per-file cap keeps only the newest MaxPerFile entries
// and garbage-collects the dropped entries' now-orphaned objects.
func TestPruneCount(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	s.MaxPerFile = 3
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	tick := 0
	s.now = func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Minute) }

	path := "/tmp/prune.txt"
	for i := 0; i < 5; i++ {
		s.Record(path, []byte{byte('a' + i), '\n'})
	}
	entries := s.List(path)
	if len(entries) != 3 {
		t.Fatalf("List = %d entries, want 3 (cap)", len(entries))
	}
	if got, _ := s.Read(entries[0].Hash); string(got) != "e\n" {
		t.Fatalf("newest = %q, want %q", got, "e\n")
	}
	if got, _ := s.Read(entries[2].Hash); string(got) != "c\n" {
		t.Fatalf("oldest kept = %q, want %q", got, "c\n")
	}
	objs, _ := os.ReadDir(filepath.Join(dir, "objects"))
	if len(objs) != 3 {
		t.Fatalf("objects = %d after gc, want 3", len(objs))
	}
}

// TestPruneAge: entries older than MaxAge drop out on the next Record.
func TestPruneAge(t *testing.T) {
	s := New(t.TempDir())
	s.MaxAge = time.Hour
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	path := "/tmp/age.txt"
	s.Record(path, []byte("old\n"))
	now = now.Add(2 * time.Hour)
	s.Record(path, []byte("new\n"))

	entries := s.List(path)
	if len(entries) != 1 {
		t.Fatalf("List = %d entries, want 1 (aged out)", len(entries))
	}
	if got, _ := s.Read(entries[0].Hash); string(got) != "new\n" {
		t.Fatalf("survivor = %q, want %q", got, "new\n")
	}
}

// TestSharedContentAcrossFiles: the same content in two files shares one
// object, and gc keeps it while either file still references it.
func TestSharedContentAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	s.MaxPerFile = 1
	s.Record("/tmp/a.txt", []byte("shared\n"))
	s.Record("/tmp/b.txt", []byte("shared\n"))
	objs, _ := os.ReadDir(filepath.Join(dir, "objects"))
	if len(objs) != 1 {
		t.Fatalf("objects = %d, want 1 (content-addressed)", len(objs))
	}
	// a.txt moves on; the shared blob survives via b.txt's reference.
	s.Record("/tmp/a.txt", []byte("changed\n"))
	if got, _ := s.Read(Hash([]byte("shared\n"))); string(got) != "shared\n" {
		t.Fatalf("shared blob gone despite live reference (got %q)", got)
	}
}

// TestPathSpellingsShareHistory: relative and absolute spellings of the same
// path key one history.
func TestPathSpellingsShareHistory(t *testing.T) {
	s := New(t.TempDir())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	s.Record("f.txt", []byte("v1\n"))
	if n := len(s.List(filepath.Join(wd, "f.txt"))); n != 1 {
		t.Fatalf("absolute spelling sees %d entries, want 1", n)
	}
}

// TestMissingStoreIsQuiet: a zero/empty store neither panics nor lists.
func TestMissingStoreIsQuiet(t *testing.T) {
	var s *Store
	s.Record("/tmp/x", []byte("v"))
	if s.List("/tmp/x") != nil {
		t.Fatal("nil store listed entries")
	}
	empty := &Store{}
	empty.Record("/tmp/x", []byte("v"))
	if entries := empty.List("/tmp/x"); len(entries) != 0 {
		t.Fatal("dirless store recorded entries")
	}
}

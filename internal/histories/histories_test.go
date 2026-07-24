package histories

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	return NewAt(filepath.Join(t.TempDir(), "histories.json"))
}

// Push orders newest first and All returns a copy.
func TestPushOrder(t *testing.T) {
	s := tempStore(t)
	s.Push(Search, "one")
	s.Push(Search, "two")
	s.Push(Search, "three")
	got := s.All(Search)
	want := []string{"three", "two", "one"}
	if len(got) != len(want) {
		t.Fatalf("All = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("All = %v, want %v", got, want)
		}
	}
	got[0] = "mutated"
	if s.All(Search)[0] != "three" {
		t.Fatal("All must return a copy")
	}
}

// Re-pushing an existing query moves it to the front instead of duplicating.
func TestPushDedupes(t *testing.T) {
	s := tempStore(t)
	s.Push(Search, "a")
	s.Push(Search, "b")
	s.Push(Search, "a")
	got := s.All(Search)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("All = %v, want [a b]", got)
	}
	// Consecutive duplicate is a no-op beyond the move-to-front.
	s.Push(Search, "a")
	if got := s.All(Search); len(got) != 2 || got[0] != "a" {
		t.Fatalf("consecutive dup: All = %v", got)
	}
}

// Buckets cap at maxEntries, dropping the oldest.
func TestPushCap(t *testing.T) {
	s := tempStore(t)
	for i := 0; i < maxEntries+10; i++ {
		s.Push(Ex, fmt.Sprintf("q%d", i))
	}
	got := s.All(Ex)
	if len(got) != maxEntries {
		t.Fatalf("len = %d, want %d", len(got), maxEntries)
	}
	if got[0] != fmt.Sprintf("q%d", maxEntries+9) {
		t.Fatalf("newest = %q", got[0])
	}
	if got[maxEntries-1] != "q10" {
		t.Fatalf("oldest = %q, want q10", got[maxEntries-1])
	}
}

// Empty queries and bucket names are ignored.
func TestPushIgnoresEmpty(t *testing.T) {
	s := tempStore(t)
	s.Push(Search, "")
	s.Push("", "x")
	if len(s.All(Search)) != 0 {
		t.Fatal("empty push must be ignored")
	}
}

// Buckets are independent.
func TestBucketsIndependent(t *testing.T) {
	s := tempStore(t)
	s.Push(Search, "pattern")
	s.Push(Ex, "w file")
	s.Push(FindInPath, "todo")
	if got := s.All(Search); len(got) != 1 || got[0] != "pattern" {
		t.Fatalf("search = %v", got)
	}
	if got := s.All(Ex); len(got) != 1 || got[0] != "w file" {
		t.Fatalf("ex = %v", got)
	}
	if got := s.All(FindInPath); len(got) != 1 || got[0] != "todo" {
		t.Fatalf("findInPath = %v", got)
	}
}

// A push persists; a fresh store at the same file reads it back.
func TestPersistRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "histories.json")
	s := NewAt(file)
	s.Push(Search, "alpha")
	s.Push(Ex, "beta")

	fresh := NewAt(file)
	if got := fresh.All(Search); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("search = %v", got)
	}
	if got := fresh.All(Ex); len(got) != 1 || got[0] != "beta" {
		t.Fatalf("ex = %v", got)
	}
}

// A malformed file reads as empty and never errors.
func TestMalformedFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "histories.json")
	if err := os.WriteFile(file, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewAt(file)
	if len(s.All(Search)) != 0 {
		t.Fatal("malformed file must read as empty")
	}
	s.Push(Search, "x")
	if got := NewAt(file).All(Search); len(got) != 1 || got[0] != "x" {
		t.Fatalf("recovered store = %v", got)
	}
}

// A version bump reads as empty; empty entries are dropped on load.
func TestLoadValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "histories.json")
	data, _ := json.Marshal(envelope{Version: 99, Buckets: map[string][]string{Search: {"x"}}})
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if len(NewAt(file).All(Search)) != 0 {
		t.Fatal("future version must read as empty")
	}
	data, _ = json.Marshal(envelope{Version: version, Buckets: map[string][]string{Search: {"", "ok", ""}}})
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := NewAt(file).All(Search); len(got) != 1 || got[0] != "ok" {
		t.Fatalf("All = %v, want [ok]", got)
	}
}

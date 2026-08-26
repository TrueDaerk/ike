package mru

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBumpRankAndDedupe(t *testing.T) {
	s := Load("")
	s.Bump("go", "alpha")
	s.Bump("go", "beta")
	s.Bump("go", "alpha") // moves back to front, no duplicate
	if r := s.Rank("go", "alpha"); r != 0 {
		t.Fatalf("alpha rank = %d, want 0", r)
	}
	if r := s.Rank("go", "beta"); r != 1 {
		t.Fatalf("beta rank = %d, want 1", r)
	}
	if r := s.Rank("go", "gamma"); r != -1 {
		t.Fatalf("absent rank = %d, want -1", r)
	}
}

// TestScopesAreIndependent guards #2146: an accept in one language never
// boosts another language's popup.
func TestScopesAreIndependent(t *testing.T) {
	s := Load("")
	s.Bump("go", "handler")
	if r := s.Rank("python", "handler"); r != -1 {
		t.Fatalf("cross-scope rank = %d, want -1", r)
	}
	if r := s.Rank("go", "handler"); r != 0 {
		t.Fatalf("own-scope rank = %d, want 0", r)
	}
}

// TestGlobalFallback guards the #2146 migration shape: a label known only to
// the "" scope still ranks under a named scope until that scope learns it.
func TestGlobalFallback(t *testing.T) {
	s := Load("")
	s.Bump("", "legacy")
	if r := s.Rank("go", "legacy"); r != 0 {
		t.Fatalf("fallback rank = %d, want 0", r)
	}
	s.Bump("go", "own")
	if r := s.Rank("go", "own"); r != 0 {
		t.Fatalf("scoped rank = %d, want 0", r)
	}
}

func TestCap(t *testing.T) {
	s := Load("")
	for i := 0; i < maxEntries+20; i++ {
		s.Bump("go", string(rune('a'+i%26))+string(rune('0'+i%10))+"x"+string(rune('A'+i%26))+string(rune('a'+(i/26)%26)))
	}
	s.mu.Lock()
	n := len(s.scopes["go"])
	s.mu.Unlock()
	if n > maxEntries {
		t.Fatalf("labels = %d, want ≤ %d", n, maxEntries)
	}
}

func TestNilSafe(t *testing.T) {
	var s *Store
	s.Bump("go", "x")
	if r := s.Rank("go", "x"); r != -1 {
		t.Fatalf("nil store rank = %d", r)
	}
}

func TestPersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "mru.json")
	s := Load(path)
	s.Bump("go", "persisted")
	// Bump saves asynchronously; poll for the reload to see it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if Load(path).Rank("go", "persisted") == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("bump never persisted")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestLegacyArrayLoads guards the #2146 migration: a pre-scope flat label
// array loads into the "" scope and keeps boosting through the fallback.
func TestLegacyArrayLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mru.json")
	data, _ := json.Marshal([]string{"old_one", "old_two"})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	s := Load(path)
	if r := s.Rank("", "old_two"); r != 1 {
		t.Fatalf("legacy global rank = %d, want 1", r)
	}
	if r := s.Rank("go", "old_one"); r != 0 {
		t.Fatalf("legacy fallback rank = %d, want 0", r)
	}
}

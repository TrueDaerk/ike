package mru

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBumpRankAndDedupe(t *testing.T) {
	s := Load("")
	s.Bump("alpha")
	s.Bump("beta")
	s.Bump("alpha") // moves back to front, no duplicate
	if r := s.Rank("alpha"); r != 0 {
		t.Fatalf("alpha rank = %d, want 0", r)
	}
	if r := s.Rank("beta"); r != 1 {
		t.Fatalf("beta rank = %d, want 1", r)
	}
	if r := s.Rank("gamma"); r != -1 {
		t.Fatalf("absent rank = %d, want -1", r)
	}
}

func TestCap(t *testing.T) {
	s := Load("")
	for i := 0; i < maxEntries+20; i++ {
		s.Bump(string(rune('a'+i%26)) + string(rune('0'+i%10)) + "x" + string(rune('A'+i%26)) + string(rune('a'+(i/26)%26)))
	}
	s.mu.Lock()
	n := len(s.labels)
	s.mu.Unlock()
	if n > maxEntries {
		t.Fatalf("labels = %d, want ≤ %d", n, maxEntries)
	}
}

func TestNilSafe(t *testing.T) {
	var s *Store
	s.Bump("x")
	if r := s.Rank("x"); r != -1 {
		t.Fatalf("nil store rank = %d", r)
	}
}

func TestPersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "mru.json")
	s := Load(path)
	s.Bump("persisted")
	// Bump saves asynchronously; poll for the reload to see it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if Load(path).Rank("persisted") == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("bump never persisted")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

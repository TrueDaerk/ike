// Package mru is the recently-accepted-completions store (Roadmap 0410,
// #854): the labels of the last accepted completion items, most recent first,
// persisted per project. The editor boosts these in the popup ranking — the
// JetBrains "you picked this before" effect.
package mru

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// maxEntries bounds the list; deep ranks carry no boost anyway.
const maxEntries = 100

// Store is a most-recent-first label list with atomic-file persistence.
// The zero path makes it memory-only (tests, no project).
type Store struct {
	mu     sync.Mutex
	labels []string
	path   string
}

// DefaultFile is the per-project store location, next to the other .ike state.
func DefaultFile() string { return filepath.Join(".ike", "completion-mru.json") }

// Load reads the store at path; a missing or unreadable file starts empty.
func Load(path string) *Store {
	s := &Store{path: path}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var labels []string
	if json.Unmarshal(data, &labels) == nil {
		if len(labels) > maxEntries {
			labels = labels[:maxEntries]
		}
		s.labels = labels
	}
	return s
}

// Bump moves label to the front (inserting it if new) and persists
// asynchronously. Safe from the UI goroutine.
func (s *Store) Bump(label string) {
	if s == nil || label == "" {
		return
	}
	s.mu.Lock()
	out := make([]string, 0, len(s.labels)+1)
	out = append(out, label)
	for _, l := range s.labels {
		if l != label {
			out = append(out, l)
		}
	}
	if len(out) > maxEntries {
		out = out[:maxEntries]
	}
	s.labels = out
	path := s.path
	snapshot := make([]string, len(out))
	copy(snapshot, out)
	s.mu.Unlock()
	if path == "" {
		return
	}
	go save(path, snapshot)
}

// Rank returns label's recency rank (0 = most recent) or -1 when absent.
func (s *Store) Rank(label string) int {
	if s == nil {
		return -1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, l := range s.labels {
		if l == label {
			return i
		}
	}
	return -1
}

// save writes the snapshot atomically (temp + rename); errors are dropped —
// the store is a ranking hint, not data.
func save(path string, labels []string) {
	data, err := json.Marshal(labels)
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".mru-*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	if tmp.Close() != nil {
		os.Remove(name)
		return
	}
	if os.Rename(name, path) != nil {
		os.Remove(name)
	}
}

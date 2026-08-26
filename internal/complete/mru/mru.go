// Package mru is the recently-accepted-completions store (Roadmap 0410,
// #854): the labels of the last accepted completion items, most recent first,
// persisted per project and scoped per language (#2146) — an accept in a Go
// buffer boosts Go completions, not the unrelated Python popup. The editor
// boosts these in the popup ranking — the JetBrains "you picked this before"
// effect.
package mru

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// maxEntries bounds each scope's list; deep ranks carry no boost anyway.
const maxEntries = 100

// Store is a per-scope most-recent-first label list with atomic-file
// persistence. The scope is the buffer's language id ("" for an unclassified
// buffer). The zero path makes it memory-only (tests, no project).
type Store struct {
	mu     sync.Mutex
	scopes map[string][]string
	path   string
}

// DefaultFile is the per-project store location, next to the other .ike state.
func DefaultFile() string { return filepath.Join(".ike", "completion-mru.json") }

// Load reads the store at path; a missing or unreadable file starts empty. A
// pre-#2146 file (a flat label array) loads into the "" scope, so old boosts
// survive as the fallback until the per-language lists fill up.
func Load(path string) *Store {
	s := &Store{path: path, scopes: map[string][]string{}}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var scopes map[string][]string
	if json.Unmarshal(data, &scopes) == nil {
		for k, labels := range scopes {
			if len(labels) > maxEntries {
				labels = labels[:maxEntries]
			}
			s.scopes[k] = labels
		}
		return s
	}
	var legacy []string
	if json.Unmarshal(data, &legacy) == nil {
		if len(legacy) > maxEntries {
			legacy = legacy[:maxEntries]
		}
		s.scopes[""] = legacy
	}
	return s
}

// Bump moves label to the front of scope's list (inserting it if new) and
// persists asynchronously. Safe from the UI goroutine.
func (s *Store) Bump(scope, label string) {
	if s == nil || label == "" {
		return
	}
	s.mu.Lock()
	prev := s.scopes[scope]
	out := make([]string, 0, len(prev)+1)
	out = append(out, label)
	for _, l := range prev {
		if l != label {
			out = append(out, l)
		}
	}
	if len(out) > maxEntries {
		out = out[:maxEntries]
	}
	s.scopes[scope] = out
	path := s.path
	var snapshot map[string][]string
	if path != "" {
		snapshot = make(map[string][]string, len(s.scopes))
		for k, labels := range s.scopes {
			cp := make([]string, len(labels))
			copy(cp, labels)
			snapshot[k] = cp
		}
	}
	s.mu.Unlock()
	if path == "" {
		return
	}
	go save(path, snapshot)
}

// Rank returns label's recency rank in scope (0 = most recent) or -1 when
// absent. A named scope with no hit falls back to the "" scope, so legacy
// (pre-#2146) global entries keep boosting until the language list fills.
func (s *Store) Rank(scope, label string) int {
	if s == nil {
		return -1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := rankIn(s.scopes[scope], label); r >= 0 {
		return r
	}
	if scope != "" {
		return rankIn(s.scopes[""], label)
	}
	return -1
}

// rankIn is label's index in labels, or -1.
func rankIn(labels []string, label string) int {
	for i, l := range labels {
		if l == label {
			return i
		}
	}
	return -1
}

// save writes the snapshot atomically (temp + rename); errors are dropped —
// the store is a ranking hint, not data.
func save(path string, scopes map[string][]string) {
	data, err := json.Marshal(scopes)
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

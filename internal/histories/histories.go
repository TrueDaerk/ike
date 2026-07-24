// Package histories persists query-recall lists (#1171): the editor's "/"
// and "?" search line, its ":" ex line, and the find-in-path query input each
// keep a named bucket (newest first) in one histories.json under the state
// store (the same root the session/undo/marks stores use: IKE_CONFIG_DIR when
// set, otherwise the project's ".ike" directory). The store loads lazily on
// first access and saves on every push — pushes are rare (one per committed
// query), so no debounce is needed. Mirroring the marks-store trade-off
// (#1151): failing to persist must never disrupt editing, so errors are
// swallowed and a malformed file simply reads as empty.
package histories

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const version = 1

// maxEntries bounds every bucket; the oldest entries drop first.
const maxEntries = 50

// Bucket names. Consumers must use these constants so the on-disk keys stay
// stable.
const (
	// Search is the editor's in-file "/" and "?" line (one shared bucket —
	// vim keeps a single search history for both directions).
	Search = "search"
	// Ex is the editor's ":" command line. It shares the input code path
	// with Search (#1110) but recalls independently, like vim's separate
	// ":" history.
	Ex = "ex"
	// FindInPath is the find-in-path overlay's query field.
	FindInPath = "findInPath"
)

// envelope is the on-disk schema: named buckets, newest entry first.
type envelope struct {
	Version int                 `json:"version"`
	Buckets map[string][]string `json:"buckets"`
}

// Store holds the recall buckets. The zero value is ready to use; the file is
// read on first access.
type Store struct {
	loaded  bool
	buckets map[string][]string
	// file overrides the on-disk location (tests); empty means the default.
	file string
}

// NewAt returns a store persisting to the given file — the test seam; the
// zero value uses the default state-store location.
func NewAt(file string) *Store { return &Store{file: file} }

// path returns the store file: IKE_CONFIG_DIR overrides the base directory
// (tests redirect writes), otherwise the project's ".ike".
func (s *Store) path() string {
	if s.file != "" {
		return s.file
	}
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "histories.json")
	}
	return filepath.Join(".ike", "histories.json")
}

// ensure loads the file once; anything malformed reads as empty.
func (s *Store) ensure() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.buckets = map[string][]string{}
	data, err := os.ReadFile(s.path())
	if err != nil {
		return
	}
	var env envelope
	if json.Unmarshal(data, &env) != nil || env.Version != version {
		return
	}
	for name, entries := range env.Buckets {
		var clean []string
		for _, e := range entries {
			if e != "" && len(clean) < maxEntries {
				clean = append(clean, e)
			}
		}
		if name != "" && len(clean) > 0 {
			s.buckets[name] = clean
		}
	}
}

// save writes the current buckets; errors are swallowed (see package comment).
func (s *Store) save() {
	file := s.path()
	if len(s.buckets) == 0 {
		_ = os.Remove(file)
		return
	}
	data, err := json.Marshal(envelope{Version: version, Buckets: s.buckets})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(file, data, 0o644)
}

// Push records q at the front of bucket and persists. Any earlier occurrence
// of the same query is removed (vim's history behavior — recommitting an old
// query moves it to the front instead of duplicating it), and the bucket is
// capped at maxEntries. Empty queries are ignored.
func (s *Store) Push(bucket, q string) {
	if bucket == "" || q == "" {
		return
	}
	s.ensure()
	out := make([]string, 0, len(s.buckets[bucket])+1)
	out = append(out, q)
	for _, h := range s.buckets[bucket] {
		if h != q {
			out = append(out, h)
		}
	}
	if len(out) > maxEntries {
		out = out[:maxEntries]
	}
	s.buckets[bucket] = out
	s.save()
}

// All returns a copy of bucket's entries, newest first.
func (s *Store) All(bucket string) []string {
	s.ensure()
	return append([]string(nil), s.buckets[bucket]...)
}

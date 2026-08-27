// Package frecency is the shared "frequency + recency" scorer behind ranked
// pickers (#2155): a small persisted store that answers "how hot is this key
// right now?" from the events the caller reports. It is deliberately generic —
// keys are opaque strings, so the '@' file finder keys it by absolute file
// path while a future command-history ranking can key it by command id.
//
// The model is a single decaying accumulator per key, the shape zoxide uses.
// Every event adds 1.0 to the key's weight after the old weight has been aged
// down to the event's instant; reading a score ages the stored weight to
// "now". So a key touched twice today outranks one touched twice a month ago,
// and one touched twenty times last week outranks one touched once yesterday —
// frequency and recency in one number, without keeping an event log.
//
// The store persists as JSON and tolerates a missing or malformed file: a
// ranking aid must never disrupt the session, so every failure degrades to
// "no frecency data" rather than to an error.
package frecency

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// HalfLife is the age at which a past event counts half as much as a fresh
// one. Two weeks keeps a file one is working on this sprint clearly ahead of
// last month's detour while never fully erasing an old favourite.
const HalfLife = 14 * 24 * time.Hour

// MaxEntries caps the store: a long-lived project touches far more files than
// are worth remembering, and an unbounded map is an unbounded write on every
// event. Once the cap is exceeded the coldest entries (lowest current score)
// are dropped — they are precisely the ones ranking would not have surfaced.
const MaxEntries = 500

// entry is one key's decaying accumulator: Weight as of the Last event.
type entry struct {
	Weight float64   `json:"w"`
	Last   time.Time `json:"t"`
}

// Store is the persisted per-key frecency accumulator. The zero value (and a
// nil pointer) is inert: Score returns 0 and Bump is a no-op, so a caller that
// never wired a store needs no nil checks.
type Store struct {
	path    string
	entries map[string]entry
	// now overrides the clock; tests only. Nil means the wall clock.
	now func() time.Time
}

// Load reads the store at path, tolerating a missing or malformed file (fresh,
// empty state). An empty path builds an in-memory store that never persists.
func Load(path string) *Store {
	s := &Store{path: path, entries: map[string]entry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var entries map[string]entry
	if json.Unmarshal(data, &entries) == nil && entries != nil {
		for k, e := range entries {
			// Drop structurally impossible records a hand-edited or
			// truncated file may carry, rather than letting them poison the
			// ranking with NaN/±Inf comparisons.
			if k == "" || math.IsNaN(e.Weight) || math.IsInf(e.Weight, 0) || e.Weight < 0 {
				continue
			}
			s.entries[k] = e
		}
	}
	return s
}

// Key normalizes a filesystem path into a stable store key: cleaned and made
// absolute against the process working directory. File-open events reach the
// store as project-relative paths from the finder and as whatever the opening
// site held (relative or absolute), so both sides must agree on one spelling —
// otherwise a bumped file would never be found again when ranking. Callers
// keying the store by something else (a command id) pass it through unchanged.
func Key(path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		if cwd, err := os.Getwd(); err == nil {
			return filepath.Join(cwd, path)
		}
	}
	return filepath.Clean(path)
}

// SetClock installs a deterministic time source; tests only.
func (s *Store) SetClock(now func() time.Time) {
	if s != nil {
		s.now = now
	}
}

// clock returns the injectable now source, defaulting to the wall clock.
func (s *Store) clock() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

// Decay is the weight multiplier for an event of the given age: 1 at age zero,
// 0.5 at one half-life, halving again per further half-life. A negative age (a
// clock that jumped backwards, or a file written by a machine ahead of this
// one) does not amplify — it is treated as fresh.
func Decay(age time.Duration) float64 {
	if age <= 0 {
		return 1
	}
	return math.Pow(0.5, age.Seconds()/HalfLife.Seconds())
}

// Bump records one event for key and persists the store. The stored weight is
// first aged to now, then incremented by one, so the accumulator always means
// "events so far, discounted by their age". Errors are swallowed: failing to
// persist must never disrupt the session.
func (s *Store) Bump(key string) {
	if s == nil || key == "" {
		return
	}
	if s.entries == nil {
		s.entries = map[string]entry{}
	}
	now := s.clock()
	e := s.entries[key]
	s.entries[key] = entry{Weight: e.Weight*Decay(now.Sub(e.Last)) + 1, Last: now}
	if e.Last.IsZero() {
		// New key: the cap can only be crossed by an insertion.
		s.prune(now)
	}
	s.save()
}

// Score returns key's current frecency: the stored weight decayed to now. An
// unknown key scores 0, so a never-opened file is simply cold rather than
// special-cased by callers.
func (s *Store) Score(key string) float64 {
	if s == nil || key == "" {
		return 0
	}
	e, ok := s.entries[key]
	if !ok {
		return 0
	}
	return e.Weight * Decay(s.clock().Sub(e.Last))
}

// Len reports how many keys the store holds; used by tests and the cap.
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	return len(s.entries)
}

// prune drops the coldest entries until the store fits MaxEntries. Ties break
// by key so pruning is deterministic.
func (s *Store) prune(now time.Time) {
	if len(s.entries) <= MaxEntries {
		return
	}
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	score := func(k string) float64 {
		e := s.entries[k]
		return e.Weight * Decay(now.Sub(e.Last))
	}
	sort.Slice(keys, func(i, j int) bool {
		si, sj := score(keys[i]), score(keys[j])
		if si != sj {
			return si > sj
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys[MaxEntries:] {
		delete(s.entries, k)
	}
}

// save writes the store, creating its directory if needed. Every error is
// swallowed — an unwritable state file costs ranking quality, nothing more.
func (s *Store) save() {
	if s.path == "" {
		return
	}
	data, err := json.Marshal(s.entries)
	if err != nil {
		return
	}
	if dir := filepath.Dir(s.path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(s.path, data, 0o644)
}

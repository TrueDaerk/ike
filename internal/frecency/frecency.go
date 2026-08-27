// Package frecency is the shared "frequency + recency" store behind ranked
// pickers: a small persisted, per-key history of events folded into a decayed
// count. Keys are opaque strings, so the palette's command mode keys it by
// command id (#2153) while the '@' file finder keys it by file path (#2155) —
// one implementation, one on-disk format, two stores.
//
// Each key keeps the unix timestamps of its recent events, ascending. A score
// sums 0.5^(age/halfLife) over them, so frequency and recency blend into one
// number: a command run three times today outscores one run five times last
// month, and a file opened all week outranks one opened once yesterday. The
// half-life is per store — how fast "lately" fades differs between what a
// project runs and what it edits.
//
// The store persists as JSON and tolerates a missing or malformed file: a
// ranking aid must never disrupt the session, so every failure degrades to
// "no history" rather than to an error.
package frecency

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// MaxKeys caps how many keys keep a history; the lowest-scoring entries
	// are pruned once the store grows past it, so a long-lived project cannot
	// grow the file without bound.
	MaxKeys = 400
	// MaxHits caps the timestamps kept per key — the oldest are dropped
	// first. Beyond a handful, older hits barely move a decayed sum.
	MaxHits = 16
)

// Store is the persisted per-key event history. The zero value (and nil) is
// inert: Score returns 0 and Record is a no-op without a path, so a caller
// that never wired a store needs no nil checks.
type Store struct {
	path     string
	halfLife time.Duration
	now      func() time.Time
	hits     map[string][]int64
}

// Load reads the history file at path, tolerating a missing or malformed file
// (fresh history). halfLife is the age at which one event is worth half of a
// fresh one; a non-positive value falls back to a week. An empty path builds
// an in-memory store that never persists.
func Load(path string, halfLife time.Duration) *Store {
	if halfLife <= 0 {
		halfLife = 7 * 24 * time.Hour
	}
	f := &Store{path: path, halfLife: halfLife, hits: map[string][]int64{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return f
	}
	var hits map[string][]int64
	if json.Unmarshal(data, &hits) == nil && hits != nil {
		for id, ts := range hits {
			if id == "" || len(ts) == 0 {
				continue // a truncated or hand-edited entry contributes nothing
			}
			sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
			f.hits[id] = trimHits(ts)
		}
	}
	return f
}

// SetNow overrides the clock, so tests can age history deterministically.
func (f *Store) SetNow(now func() time.Time) {
	if f != nil {
		f.now = now
	}
}

// clock returns the store's time source, defaulting to the wall clock.
func (f *Store) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// Record appends one event for the key and persists the history. Errors are
// swallowed: failing to persist must never disrupt the session.
func (f *Store) Record(id string) {
	if f == nil || id == "" {
		return
	}
	if f.hits == nil {
		f.hits = map[string][]int64{}
	}
	f.hits[id] = trimHits(append(f.hits[id], f.clock().Unix()))
	f.prune()
	f.save()
}

// Score returns the decayed event count of the key: each recorded hit
// contributes 0.5^(age/halfLife). Unknown keys score 0, so a never-touched
// entry is simply cold rather than special-cased by callers.
func (f *Store) Score(id string) float64 {
	if f == nil || id == "" {
		return 0
	}
	ts := f.hits[id]
	if len(ts) == 0 {
		return 0
	}
	now := f.clock()
	total := 0.0
	for _, t := range ts {
		age := now.Sub(time.Unix(t, 0))
		if age < 0 {
			age = 0 // a clock step backwards must not inflate the score
		}
		total += math.Pow(0.5, age.Seconds()/f.halfLife.Seconds())
	}
	return total
}

// Len reports how many keys the store tracks.
func (f *Store) Len() int {
	if f == nil {
		return 0
	}
	return len(f.hits)
}

// Key normalizes a filesystem path into a stable store key: cleaned and made
// absolute against the process working directory. File-open events reach the
// store as project-relative paths from the finder and as whatever the opening
// site held (relative or absolute), so both sides must agree on one spelling —
// otherwise a recorded file would never be found again when ranking. Callers
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

// trimHits keeps at most MaxHits timestamps, dropping the oldest.
func trimHits(ts []int64) []int64 {
	if len(ts) <= MaxHits {
		return ts
	}
	return append([]int64(nil), ts[len(ts)-MaxHits:]...)
}

// prune caps the number of tracked keys, dropping the lowest-scoring ones so
// the store cannot grow without bound over a project's lifetime.
func (f *Store) prune() {
	if len(f.hits) <= MaxKeys {
		return
	}
	ids := make([]string, 0, len(f.hits))
	for id := range f.hits {
		ids = append(ids, id)
	}
	// Score descending, id ascending, so the drop set is deterministic.
	sort.Slice(ids, func(i, j int) bool {
		si, sj := f.Score(ids[i]), f.Score(ids[j])
		if si != sj {
			return si > sj
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids[MaxKeys:] {
		delete(f.hits, id)
	}
}

// save writes the history back, best effort.
func (f *Store) save() {
	if f.path == "" {
		return
	}
	data, err := json.Marshal(f.hits)
	if err != nil {
		return
	}
	if dir := filepath.Dir(f.path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(f.path, data, 0o644)
}

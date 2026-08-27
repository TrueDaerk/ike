package palette

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// frecency.go implements the command-execution frecency store (#2153):
// a per-project, timestamped history of *every* command dispatch — palette
// selection, keybind, or inline invocation alike, because the root model
// records it in the single dispatchCommand funnel — folded into a
// frequency-plus-recency-decay score command mode blends into its ranking.
//
// It is deliberately separate from the most-used counter in usage.go (#773):
// that one counts palette-window selections only and stays a plain tiebreaker,
// while this one answers "what does this project actually run, lately".

const (
	// frecencyHalfLife is the age at which one execution is worth half of a
	// fresh one. A week keeps yesterday's work dominant without erasing the
	// commands a project reaches for every few days.
	frecencyHalfLife = 7 * 24 * time.Hour
	// frecencyMaxIDs caps how many commands keep a history; the lowest-scoring
	// entries are pruned once the store grows past it.
	frecencyMaxIDs = 400
	// frecencyMaxHits caps the timestamps kept per command — the oldest are
	// dropped first. Beyond a handful, older hits barely move a decayed sum.
	frecencyMaxHits = 16
)

// Frecency is the persisted command-execution history: per command id, the
// unix timestamps of its recent executions, ascending. The zero value (and
// nil) is inert: Score returns 0 and Record is a no-op without a path.
type Frecency struct {
	path string
	now  func() time.Time
	hits map[string][]int64
}

// LoadFrecency reads the history file at path, tolerating a missing or
// malformed file (fresh history). Failing to read must never disrupt the
// palette, so every error yields an empty-but-usable store.
func LoadFrecency(path string) *Frecency {
	f := &Frecency{path: path, hits: map[string][]int64{}}
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
func (f *Frecency) SetNow(now func() time.Time) {
	if f != nil {
		f.now = now
	}
}

// clock returns the store's time source, defaulting to the wall clock.
func (f *Frecency) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// Record appends one execution of the command and persists the history.
// Errors are swallowed: failing to persist must never disrupt the session.
func (f *Frecency) Record(id string) {
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

// Score returns the decayed execution count of the command: each recorded hit
// contributes 0.5^(age/halfLife), so a command run three times today outscores
// one run five times last month. Unknown ids score 0.
func (f *Frecency) Score(id string) float64 {
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
		total += math.Pow(0.5, age.Seconds()/frecencyHalfLife.Seconds())
	}
	return total
}

// trimHits keeps at most frecencyMaxHits timestamps, dropping the oldest.
func trimHits(ts []int64) []int64 {
	if len(ts) <= frecencyMaxHits {
		return ts
	}
	return append([]int64(nil), ts[len(ts)-frecencyMaxHits:]...)
}

// prune caps the number of tracked commands, dropping the lowest-scoring ones
// so the store cannot grow without bound over a project's lifetime.
func (f *Frecency) prune() {
	if len(f.hits) <= frecencyMaxIDs {
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
	for _, id := range ids[frecencyMaxIDs:] {
		delete(f.hits, id)
	}
}

// save writes the history back, best effort.
func (f *Frecency) save() {
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

// Frecency boost weights. The boost is added to a command's fuzzy score before
// sorting, scaled by how much the query itself has to say: on an empty query
// every fuzzy score is 0, so history alone orders the listing; each further
// typed rune halves the boost, so by a handful of runes the match quality
// dominates and history only breaks ties.
const (
	frecencyEmptyWeight = 1000 // empty query: history is the only signal
	frecencyBaseWeight  = 24   // one-rune query, halved per further rune
)

// frecencyBoost converts a decayed execution count into a sort bonus for a
// query of queryLen runes. The count is squashed into [0,1) first, so a much-
// used command cannot outweigh the query-length damping by sheer volume.
func frecencyBoost(score float64, queryLen int) float64 {
	if score <= 0 {
		return 0
	}
	norm := 1 - math.Pow(0.5, score)
	if queryLen == 0 {
		return norm * frecencyEmptyWeight
	}
	return norm * frecencyBaseWeight / math.Pow(2, float64(queryLen-1))
}

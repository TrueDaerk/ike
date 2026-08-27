package palette

import (
	"math"
	"time"

	"ike/internal/frecency"
)

// frecency.go wires the palette to the shared frecency store
// (`internal/frecency`, #2155): a per-project, timestamped history of events
// folded into a frequency-plus-recency-decay score. The palette keeps two of
// them, differing only in key space and half-life:
//
//   - command mode (#2153) keys it by command id and records *every* dispatch
//     — palette selection, keybind, or inline invocation alike, because the
//     root model records it in the single dispatchCommand funnel;
//   - the '@' file finder (#2155) keys it by absolute file path and records
//     every file open, at the same sites that feed the recent-files MRU.
//
// Both are deliberately separate from the most-used counter in usage.go
// (#773): that one counts palette-window selections only and stays a plain
// tiebreaker, while these answer "what does this project actually use, lately".
//
// This file also owns command mode's *blend policy* — how a decayed count
// becomes a sort bonus. File mode blends differently (see file_mode.go), which
// is exactly why the store itself carries no ranking opinion.

// Frecency is the palette's alias for the shared store, so existing call sites
// and the root model keep naming one type.
type Frecency = frecency.Store

const (
	// cmdHalfLife is the age at which one command execution is worth half of
	// a fresh one. A week keeps yesterday's work dominant without erasing the
	// commands a project reaches for every few days.
	cmdHalfLife = 7 * 24 * time.Hour
	// fileHalfLife is the same for file opens (#2155). Two weeks is slower on
	// purpose: what one is working *on* turns over more slowly than what one
	// runs, and a file untouched for a fortnight should fade, not vanish.
	fileHalfLife = 14 * 24 * time.Hour
)

// LoadFrecency reads the command-execution history at path (#2153).
func LoadFrecency(path string) *Frecency { return frecency.Load(path, cmdHalfLife) }

// LoadFileFrecency reads the file-open history at path (#2155) — the same
// store type on its own file and its own, slower half-life.
func LoadFileFrecency(path string) *Frecency { return frecency.Load(path, fileHalfLife) }

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

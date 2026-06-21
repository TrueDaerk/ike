// Package fuzzy is a small, dependency-free subsequence matcher used by the
// command palette (Roadmap 0070). It answers two questions at once: does pattern
// fuzzy-match text, and if so, how good is the match and which characters of
// text were hit. The matched indices let consumers highlight the same spans the
// scorer rewarded, so ranking and rendering stay consistent. The package has no
// UI or registry dependency; it is pure and deterministic.
package fuzzy

import "unicode"

// Result is the outcome of a successful fuzzy match: a Score (higher is better)
// and the rune indices in the target text that the pattern matched, in ascending
// order. Positions index runes, not bytes, so a consumer rendering the text
// rune-by-rune can highlight exactly the matched cells.
type Result struct {
	Score     int
	Positions []int
}

// Scoring weights. In order of strength: a match at a word boundary beats one
// mid-word, consecutive matches beat scattered ones, and a match anchored at the
// very start beats everything. Gaps between matched runes and a long unmatched
// lead are penalised mildly so shorter, tighter matches win.
const (
	bonusBoundary    = 16 // matched rune sits at a word boundary (start/after sep/camelHump)
	bonusConsecutive = 8  // matched rune directly follows the previous matched rune
	bonusStart       = 12 // matched rune is at index 0
	penaltyGap       = 1  // per unmatched rune between two matched runes
	penaltyLead      = 1  // per unmatched rune before the first match (capped)
	maxLeadPenalty   = 6  // ceiling on the leading-distance penalty
	negInf           = -1 << 30
)

// Match reports whether pattern is a subsequence of text (case-insensitive) and,
// when it is, returns the best score and matched rune indices. An empty pattern
// always matches with a zero score and no positions (every item passes the
// filter). The alignment is optimal: a dynamic program maximises the total score
// over all subsequence placements, so a pattern is bound to word-boundary and
// consecutive runs when they exist rather than to the earliest greedy positions.
func Match(pattern, text string) (Result, bool) {
	pr := []rune(pattern)
	if len(pr) == 0 {
		return Result{}, true
	}
	tr := []rune(text)
	P, T := len(pr), len(tr)
	if P > T {
		return Result{}, false
	}

	// best[j] is the best score matching pattern[0..i] with pattern[i] placed at
	// text rune j; parent[i][j] records the chosen previous text index for
	// reconstruction (-1 = unreachable, -2 = first row anchor).
	best := make([]int, T)
	parent := make([][]int, P)
	for i := range parent {
		parent[i] = make([]int, T)
	}

	for j := 0; j < T; j++ {
		best[j] = negInf
		parent[0][j] = -1
		if foldEqual(pr[0], tr[j]) {
			lead := j
			if lead > maxLeadPenalty {
				lead = maxLeadPenalty
			}
			best[j] = posBonus(tr, j) - lead*penaltyLead
			parent[0][j] = -2
		}
	}

	for i := 1; i < P; i++ {
		cur := make([]int, T)
		for j := 0; j < T; j++ {
			cur[j] = negInf
			parent[i][j] = -1
			if !foldEqual(pr[i], tr[j]) {
				continue
			}
			pb := posBonus(tr, j)
			for k := 0; k < j; k++ {
				if best[k] == negInf {
					continue
				}
				cand := best[k] + transition(k, j) + pb
				if cand > cur[j] {
					cur[j] = cand
					parent[i][j] = k
				}
			}
		}
		best = cur
	}

	// Pick the best end position on the final pattern row.
	endScore, end := negInf, -1
	for j := 0; j < T; j++ {
		if best[j] > endScore {
			endScore, end = best[j], j
		}
	}
	if end < 0 {
		return Result{}, false
	}

	positions := make([]int, P)
	for i := P - 1; i >= 0; i-- {
		positions[i] = end
		end = parent[i][end]
	}
	return Result{Score: endScore, Positions: positions}, true
}

// transition scores placing a matched rune at j directly after one at k:
// adjacent runes earn the consecutive bonus, a gap is penalised per skipped rune.
func transition(k, j int) int {
	if j == k+1 {
		return bonusConsecutive
	}
	return -(j - k - 1) * penaltyGap
}

// posBonus is the position-only reward for a matched rune at index j: a start
// bonus at index 0 plus a word-boundary bonus.
func posBonus(tr []rune, j int) int {
	s := 0
	if j == 0 {
		s += bonusStart
	}
	if isBoundary(tr, j) {
		s += bonusBoundary
	}
	return s
}

// isBoundary reports whether the rune at index i begins a "word" within tr: it is
// the first rune, follows a non-alphanumeric separator, or is an uppercase letter
// preceded by a lowercase one (a camelCase hump).
func isBoundary(tr []rune, i int) bool {
	if i == 0 {
		return true
	}
	prev := tr[i-1]
	if !isAlphaNum(prev) {
		return true
	}
	return unicode.IsLower(prev) && unicode.IsUpper(tr[i])
}

func isAlphaNum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// foldEqual compares two runes case-insensitively.
func foldEqual(a, b rune) bool {
	if a == b {
		return true
	}
	return unicode.ToLower(a) == unicode.ToLower(b)
}

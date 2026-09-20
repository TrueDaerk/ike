// Package fuzzy is a small, dependency-free subsequence matcher used by the
// command palette (Roadmap 0070). It answers two questions at once: does pattern
// fuzzy-match text, and if so, how good is the match and which characters of
// text were hit. The matched indices let consumers highlight the same spans the
// scorer rewarded, so ranking and rendering stay consistent. The package has no
// UI or registry dependency; it is pure and deterministic.
//
// Two matchers share one dynamic program: Match is the permissive subsequence
// matcher every picker uses, MatchHumps the JetBrains-style hump matcher the
// completion popup filters with (#2650), where a matched rune must either
// continue the previous match or start a word segment.
package fuzzy

import "unicode"

// Result is the outcome of a successful fuzzy match: a Score (higher is better)
// and the rune indices in the target text that the pattern matched, in ascending
// order. Positions index runes, not bytes, so a consumer rendering the text
// rune-by-rune can highlight exactly the matched cells. Prefix reports whether
// text starts with pattern under the matcher's case rule, whatever positions
// the scorer preferred, so a ranking tier "prefix beats hump" needs no second
// pass over the text; an empty pattern counts as a prefix.
type Result struct {
	Score     int
	Positions []int
	Prefix    bool
}

// Case is the case rule a hump match applies to the pattern (#2650,
// completion.case_sensitivity).
type Case int

const (
	// CaseFirstLetter is the IntelliJ "first letter" rule and the default: a
	// lowercase pattern rune matches either case, an uppercase pattern rune
	// only an uppercase text rune, so "DataA" cannot match "database".
	CaseFirstLetter Case = iota
	// CaseNone folds every rune: uppercase pattern runes match lowercase text.
	CaseNone
	// CaseAll compares every rune exactly.
	CaseAll
)

// ParseCase maps a completion.case_sensitivity value ("none", "first_letter",
// "all") to its Case; unknown strings fall back to CaseFirstLetter.
func ParseCase(s string) Case {
	switch s {
	case "none":
		return CaseNone
	case "all":
		return CaseAll
	}
	return CaseFirstLetter
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
	return match(pattern, text, foldEqual, false)
}

// MatchHumps is the JetBrains-style hump matcher (#2650) under the default
// CaseFirstLetter rule: see MatchHumpsCase.
func MatchHumps(pattern, text string) (Result, bool) {
	return MatchHumpsCase(pattern, text, CaseFirstLetter)
}

// MatchHumpsCase reports whether pattern hump-matches text: every matched rune
// either directly continues the previous matched rune or sits at the start of
// a word segment (index 0, after a non-alphanumeric separator, a camelCase
// hump, the last capital of an acronym before a lowercase run, or a
// letter↔digit change), and the first matched rune always starts a segment.
// So "gur" matches GotoURLResolver and "dacco" DataAccessObject, while "my"
// does not match "empty" or "summary" and "log" does not match "dialogBox".
// The Result has the same shape as Match's, so highlighting keeps working; an
// empty pattern matches everything with a zero score.
func MatchHumpsCase(pattern, text string, c Case) (Result, bool) {
	eq := foldEqual
	switch c {
	case CaseFirstLetter:
		eq = firstLetterEqual
	case CaseAll:
		eq = exactEqual
	}
	return match(pattern, text, eq, true)
}

// match is the shared dynamic program. prev[j] is the best score matching
// pattern[0..i] with pattern[i] placed at text rune j; parent[i*T+j] records the
// chosen previous text index for reconstruction (-1 = unreachable, -2 = first
// row anchor). With humps set, a rune may only be placed at a word boundary or
// directly after the previous matched rune, so the inner loop over predecessors
// collapses to the single adjacent cell for a mid-word position. Two score rows
// and one flat parent table are the only allocations besides the rune slices.
func match(pattern, text string, eq func(p, t rune) bool, humps bool) (Result, bool) {
	pr := []rune(pattern)
	if len(pr) == 0 {
		return Result{Prefix: true}, true
	}
	tr := []rune(text)
	P, T := len(pr), len(tr)
	if P > T {
		return Result{}, false
	}

	rows := make([]int, 2*T)
	prev, cur := rows[:T], rows[T:]
	parent := make([]int, P*T)

	reachable := false
	for j := 0; j < T; j++ {
		prev[j] = negInf
		parent[j] = -1
		if !eq(pr[0], tr[j]) {
			continue
		}
		boundary := isBoundary(tr, j)
		if humps && !boundary {
			continue
		}
		lead := j
		if lead > maxLeadPenalty {
			lead = maxLeadPenalty
		}
		prev[j] = posBonus(j, boundary) - lead*penaltyLead
		parent[j] = -2
		reachable = true
	}
	if !reachable {
		return Result{}, false
	}

	for i := 1; i < P; i++ {
		row := parent[i*T : (i+1)*T]
		for j := 0; j < T; j++ {
			cur[j] = negInf
			row[j] = -1
			if !eq(pr[i], tr[j]) {
				continue
			}
			boundary := isBoundary(tr, j)
			pb := posBonus(j, boundary)
			from := 0
			if humps && !boundary {
				from = j - 1 // only the consecutive predecessor is legal
			}
			for k := from; k < j; k++ {
				if k < 0 || prev[k] == negInf {
					continue
				}
				cand := prev[k] + transition(k, j) + pb
				if cand > cur[j] {
					cur[j] = cand
					row[j] = k
				}
			}
		}
		prev, cur = cur, prev
	}

	// Pick the best end position on the final pattern row.
	endScore, end := negInf, -1
	for j := 0; j < T; j++ {
		if prev[j] > endScore {
			endScore, end = prev[j], j
		}
	}
	if end < 0 {
		return Result{}, false
	}

	positions := make([]int, P)
	for i := P - 1; i >= 0; i-- {
		positions[i] = end
		end = parent[i*T+end]
	}
	prefix := true
	for i := 0; i < P; i++ {
		if !eq(pr[i], tr[i]) {
			prefix = false
			break
		}
	}
	return Result{Score: endScore, Positions: positions, Prefix: prefix}, true
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
func posBonus(j int, boundary bool) int {
	s := 0
	if j == 0 {
		s += bonusStart
	}
	if boundary {
		s += bonusBoundary
	}
	return s
}

// isBoundary reports whether the rune at index i begins a "word" within tr: it
// is the first rune, follows a non-alphanumeric separator, is an uppercase
// letter preceded by a lowercase one (a camelCase hump), is the last capital
// of an acronym run before a lowercase letter (the R of URLResolver), or sits
// on a letter↔digit change (the 2 of html2canvas, and the c after it).
func isBoundary(tr []rune, i int) bool {
	if i == 0 {
		return true
	}
	prev, cur := tr[i-1], tr[i]
	if !isAlphaNum(prev) {
		return true
	}
	if unicode.IsDigit(prev) != unicode.IsDigit(cur) {
		return true
	}
	if unicode.IsUpper(cur) {
		if unicode.IsLower(prev) {
			return true
		}
		return unicode.IsUpper(prev) && i+1 < len(tr) && unicode.IsLower(tr[i+1])
	}
	return false
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

// firstLetterEqual is the CaseFirstLetter rule: a lowercase pattern rune folds,
// any other pattern rune (uppercase, digit, separator) must match exactly.
func firstLetterEqual(p, t rune) bool {
	if p == t {
		return true
	}
	return unicode.IsLower(p) && unicode.ToLower(t) == p
}

func exactEqual(a, b rune) bool { return a == b }

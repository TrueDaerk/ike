package fuzzy

import (
	"fmt"
	"strings"
	"unicode"
)

// Class is the kind of match a candidate name is to the query. A search lists
// its candidates class by class, in the order search.match_order configures,
// and the classes never mix: the score only orders candidates of one class.
type Class int

const (
	// ClassExact: the name equals the pattern, case-insensitively and ignoring
	// separators; with ClassOptions.Extension also the name without its
	// extension ("filemode" → file_mode.go).
	ClassExact Class = iota
	// ClassPrefix: the name starts with the pattern as one contiguous run
	// ("file_m" → file_mode.go).
	ClassPrefix
	// ClassContains: the name contains the pattern as one contiguous run that
	// is not at its start ("mode" → file_mode.go).
	ClassContains
	// ClassInitials: every matched run starts at a word start of the name and
	// the match is not contiguous ("mcc" → MyCoolClass, "fimo" → file_mode.go).
	ClassInitials
	// ClassDirectory: the name does not match, the directory part of a path
	// does. Classify never returns it; a search over paths assigns it.
	ClassDirectory
	// ClassFuzzy: any other accepted match — scattered characters
	// ("flmd" → file_mode.go).
	ClassFuzzy

	classCount = int(ClassFuzzy) + 1
)

var classNames = [classCount]string{"exact", "prefix", "contains", "initials", "directory", "fuzzy"}

// String is the class's name in search.match_order.
func (c Class) String() string {
	if c < 0 || int(c) >= classCount {
		return "unknown"
	}
	return classNames[c]
}

// ClassNames lists every class name in the default ranking order.
func ClassNames() []string {
	return append([]string(nil), classNames[:]...)
}

// ParseClass maps a search.match_order element to its Class.
func ParseClass(name string) (Class, bool) {
	for i, n := range classNames {
		if n == name {
			return Class(i), true
		}
	}
	return 0, false
}

// ClassOrder is a ranking of the classes: Rank(c) is the position of c, lower
// lists first. The zero value is not a valid order; use DefaultClassOrder or
// ParseClassOrder.
type ClassOrder struct {
	rank [classCount]int
}

// DefaultClassOrder ranks the classes in their declaration order.
func DefaultClassOrder() ClassOrder {
	var o ClassOrder
	for i := range o.rank {
		o.rank[i] = i
	}
	return o
}

// ParseClassOrder reads a search.match_order value: every class name exactly
// once, the list order being the ranking. The error names the unknown,
// repeated or missing classes.
func ParseClassOrder(names []string) (ClassOrder, error) {
	var (
		o    ClassOrder
		seen [classCount]bool
	)
	for i, raw := range names {
		name := strings.TrimSpace(raw)
		c, ok := ParseClass(name)
		if !ok {
			return ClassOrder{}, fmt.Errorf("unknown match class %q (valid: %s)", name, strings.Join(classNames[:], ", "))
		}
		if seen[c] {
			return ClassOrder{}, fmt.Errorf("match class %q is listed twice", name)
		}
		seen[c] = true
		o.rank[c] = i
	}
	var missing []string
	for i, ok := range seen {
		if !ok {
			missing = append(missing, classNames[i])
		}
	}
	if len(missing) > 0 {
		return ClassOrder{}, fmt.Errorf("missing match class %s (every class is listed exactly once)", strings.Join(missing, ", "))
	}
	return o, nil
}

// Rank is the position of c in the order; lower lists first.
func (o ClassOrder) Rank(c Class) int {
	if c < 0 || int(c) >= classCount {
		return classCount
	}
	return o.rank[c]
}

// ClassOptions tunes Classify for one search.
type ClassOptions struct {
	// Case is the rule the pattern is held to. ClassExact ignores it.
	Case Case
	// Extension marks the name as a file name: the name without its extension
	// counts as the whole name for ClassExact.
	Extension bool
	// Permissive also accepts what the hump filter rejects: a contiguous run
	// that starts mid-word and nowhere else (ClassContains) and scattered
	// characters (ClassFuzzy, matched case-insensitively like Match). Without
	// it Classify accepts exactly the names MatchHumpsCase accepts.
	Permissive bool
}

// Classify reports whether pattern matches name and, when it does, the class
// of the match — the first of exact, prefix, contains, initials and fuzzy that
// applies — together with the match aligned to that class: a prefix or
// contains match scores and highlights its contiguous run, whatever alignment
// the scorer would have preferred. An empty pattern is a prefix of every name.
func Classify(pattern, name string, o ClassOptions) (Class, Result, bool) {
	pr := []rune(pattern)
	if len(pr) == 0 {
		return ClassPrefix, Result{Prefix: true}, true
	}
	tr := []rune(name)
	if len(pr) > len(tr) {
		return 0, Result{}, false
	}
	class, r, ok := classify(pattern, name, pr, tr, o)
	if !ok {
		return 0, Result{}, false
	}
	if equalsName(pattern, name, o.Extension) {
		class = ClassExact
	}
	return class, r, true
}

// classify grades the match short of the exact check.
func classify(pattern, name string, pr, tr []rune, o ClassOptions) (Class, Result, bool) {
	start, wordStart := findRun(pr, tr, caseEqual(o.Case))
	switch {
	case start == 0:
		// Scored on the prefix alone, so every name that starts with the
		// pattern scores alike whatever follows the run.
		return ClassPrefix, runResult(tr[:len(pr)], 0, len(pr)), true
	case start > 0 && (wordStart || o.Permissive):
		return ClassContains, runResult(tr, start, len(pr)), true
	}
	if hr, ok := MatchHumpsCase(pattern, name, o.Case); ok {
		if start > 0 {
			return ClassContains, runResult(tr, start, len(pr)), true
		}
		return ClassInitials, hr, true
	}
	if o.Permissive {
		if fr, ok := Match(pattern, name); ok {
			return ClassFuzzy, fr, true
		}
	}
	return 0, Result{}, false
}

// findRun locates pattern as one contiguous run in text under eq. A run at
// index 0 wins; otherwise the best-scoring run, the earliest on a tie. start
// is -1 when there is none; wordStart reports whether the run begins a word.
func findRun(pr, tr []rune, eq func(p, t rune) bool) (start int, wordStart bool) {
	start = -1
	best := negInf
	for i := 0; i+len(pr) <= len(tr); i++ {
		hit := true
		for k, p := range pr {
			if !eq(p, tr[i+k]) {
				hit = false
				break
			}
		}
		if !hit {
			continue
		}
		if i == 0 {
			return 0, true
		}
		if s := runScore(tr, i, len(pr)); s > best {
			start, best = i, s
		}
	}
	if start < 0 {
		return -1, false
	}
	return start, isBoundary(tr, start)
}

// runScore is the score the matcher gives n consecutive runes of tr matched
// from index start.
func runScore(tr []rune, start, n int) int {
	lead := start
	if lead > maxLeadPenalty {
		lead = maxLeadPenalty
	}
	s := -lead * penaltyLead
	for j := start; j < start+n; j++ {
		s += posBonus(j, isBoundary(tr, j))
		if j > start {
			s += bonusConsecutive
		}
	}
	return s
}

// runResult is the Result of a contiguous run of n runes at start.
func runResult(tr []rune, start, n int) Result {
	positions := make([]int, n)
	for i := range positions {
		positions[i] = start + i
	}
	return Result{Score: runScore(tr, start, n), Positions: positions, Prefix: start == 0}
}

// equalsName reports whether pattern spells name whole: case-insensitively
// and ignoring separators, with ext also without the extension.
func equalsName(pattern, name string, ext bool) bool {
	stem := name
	if ext {
		if i := strings.LastIndex(name, "."); i > 0 {
			stem = name[:i]
		}
	}
	if strings.EqualFold(name, pattern) || strings.EqualFold(stem, pattern) {
		return true
	}
	q := alnumFold(pattern)
	return q != "" && (q == alnumFold(name) || q == alnumFold(stem))
}

// alnumFold lowercases s and drops every rune that is not a letter or digit.
func alnumFold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// caseEqual is the rune comparison of a case rule.
func caseEqual(c Case) func(p, t rune) bool {
	switch c {
	case CaseNone:
		return foldEqual
	case CaseAll:
		return exactEqual
	}
	return firstLetterEqual
}

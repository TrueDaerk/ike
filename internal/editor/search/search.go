// Package search implements buffer search for "/" and "?" with "n"/"N"
// repetition. A Query carries the pattern and a literal-vs-regex toggle; it
// reports every match on a line (for incremental highlighting) and finds the
// next match in a direction with wrap-around. It holds no cursor state — the
// editor owns the current query and direction and passes the cursor in.
package search

import (
	"regexp"
	"strings"
	"unicode"

	"ike/internal/editor/buffer"
)

// Direction selects forward ("/") or backward ("?") search.
type Direction int

const (
	Forward Direction = iota
	Backward
)

// Case selects how a query treats letter case (#1111). CaseSmart is the
// vim-style smartcase default (#257): an all-lowercase pattern folds case,
// any uppercase rune makes it exact. CaseFold forces case-insensitive
// matching (a "\c" marker or the editor.search_ignore_case setting);
// CaseExact forces exact matching (a "\C" marker).
type Case int

const (
	CaseSmart Case = iota
	CaseFold
	CaseExact
)

// Span is a match on a single line, as rune columns [Start, End).
type Span struct {
	Line       int
	Start, End int
}

// Query is a compiled search request.
type Query struct {
	Pattern string
	Regex   bool
	fold    bool // matching ignores case (smartcase resolved at compile time)
	re      *regexp.Regexp
	jq      *jqState // structural (jq) mode (#2363, structural.go)
}

// ID identifies a compiled query for caching (#2145): two queries with equal
// IDs match exactly the same text, so a tally computed for one is valid for
// the other.
func (q Query) ID() string {
	if q.jq != nil {
		return "j:" + q.jq.langID + ":" + q.Pattern
	}
	flags := "l"
	if q.Regex {
		flags = "r"
	}
	if q.fold {
		flags += "i"
	}
	return flags + ":" + q.Pattern
}

// Compile builds a Query. When regex is true and the pattern is invalid, it
// falls back to a literal search so a half-typed regex never errors mid-keypress.
//
// cs picks the case handling (#1111): CaseSmart is vim's smartcase (#257) —
// an all-lowercase pattern matches case-insensitively, any uppercase rune
// makes it exact — while CaseFold/CaseExact force one mode regardless of the
// pattern's spelling. A case-insensitive literal runs through a quoted regex
// so multi-byte case pairs fold correctly; the exact literal keeps the
// strings.Index fast path.
func Compile(pattern string, regex bool, cs Case) Query {
	q := Query{Pattern: pattern, Regex: regex}
	if pattern == "" {
		return q
	}
	insensitive := cs == CaseFold ||
		(cs == CaseSmart && strings.IndexFunc(pattern, unicode.IsUpper) < 0)
	q.fold = insensitive
	if regex {
		expr := pattern
		if insensitive {
			expr = "(?i)" + expr
		}
		if re, err := regexp.Compile(expr); err == nil {
			q.re = re
			return q
		}
		q.Regex = false // half-typed regex: fall back to a literal search
	}
	if insensitive {
		q.re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(pattern))
	}
	return q
}

// CompileExact builds a literal Query with no smartcase folding — "*"/"#"
// search the word under the cursor exactly, vim-style.
func CompileExact(pattern string) Query {
	return Query{Pattern: pattern}
}

// Empty reports whether the query has no pattern.
func (q Query) Empty() bool { return q.Pattern == "" }

// MatchesLine reports whether the query matches anywhere in text. It is the
// allocation-free predicate behind the follow filter (#2255), which asks it
// per buffer line per frame — LineMatches would build a span slice per line
// only to have the caller throw it away.
func (q Query) MatchesLine(text string) bool {
	if q.Empty() {
		return false
	}
	if q.re != nil {
		return q.re.MatchString(text)
	}
	return strings.Contains(text, q.Pattern)
}

// LineMatches returns every match on line i as rune-column spans.
func (q Query) LineMatches(b *buffer.Buffer, i int) []Span {
	if q.jq != nil {
		return q.structuralLineMatches(i)
	}
	line := b.Line(i)
	if q.Empty() {
		return nil
	}
	var spans []Span
	if q.re != nil {
		for _, m := range q.re.FindAllStringIndex(line, -1) {
			if m[0] == m[1] {
				continue // skip empty matches
			}
			spans = append(spans, Span{Line: i, Start: runeCol(line, m[0]), End: runeCol(line, m[1])})
		}
		return spans
	}
	from := 0
	for {
		idx := strings.Index(line[from:], q.Pattern)
		if idx < 0 {
			break
		}
		bs := from + idx
		spans = append(spans, Span{Line: i, Start: runeCol(line, bs), End: runeCol(line, bs+len(q.Pattern))})
		from = bs + len(q.Pattern)
	}
	return spans
}

// AllMatches returns every match in the buffer in reading order.
func (q Query) AllMatches(b *buffer.Buffer) []Span {
	var out []Span
	for i := 0; i < b.LineCount(); i++ {
		out = append(out, q.LineMatches(b, i)...)
	}
	return out
}

// Next finds the count-th match from the cursor in dir, wrapping around the
// buffer ends. ok is false when the pattern matches nothing.
func (q Query) Next(b *buffer.Buffer, from buffer.Position, dir Direction, count int) (buffer.Position, bool) {
	all := q.AllMatches(b)
	if len(all) == 0 {
		return from, false
	}
	if count < 1 {
		count = 1
	}
	idx := -1
	if dir == Forward {
		for i, s := range all {
			if s.Line > from.Line || (s.Line == from.Line && s.Start > from.Col) {
				idx = i
				break
			}
		}
		if idx < 0 {
			idx = 0 // wrap to first
		}
		idx = (idx + count - 1) % len(all)
	} else {
		for i := len(all) - 1; i >= 0; i-- {
			s := all[i]
			if s.Line < from.Line || (s.Line == from.Line && s.Start < from.Col) {
				idx = i
				break
			}
		}
		if idx < 0 {
			idx = len(all) - 1 // wrap to last
		}
		idx = ((idx-(count-1))%len(all) + len(all)) % len(all)
	}
	m := all[idx]
	return buffer.Position{Line: m.Line, Col: m.Start}, true
}

// Match-tally caps (#2145). A tally must not cost a full scan of a very large
// buffer on every keystroke of an incremental search, so counting stops once
// either budget runs out and the result is reported as capped ("999+").
const (
	// MaxMatches is the largest exact total a tally reports.
	MaxMatches = 999
	// MaxScanLines bounds how many buffer lines one tally scans.
	MaxScanLines = 20000
)

// Tally is a capped match count for the search counter (#2145): the 1-based
// index of the match the cursor sits on (0 when it sits on none — before the
// first match, or past the scan cut) over the number of matches counted.
// Capped means the scan stopped on a budget, so Total is a lower bound and
// renders as "Total+".
type Tally struct {
	Index  int
	Total  int
	Capped bool
}

// ScanMatches returns q's matches in reading order, spending at most
// maxMatches matches and maxLines lines (non-positive values fall back to the
// MaxMatches / MaxScanLines defaults). capped reports that a budget ran out,
// so the result is a prefix of the buffer's matches rather than all of them.
func (q Query) ScanMatches(b *buffer.Buffer, maxMatches, maxLines int) (spans []Span, capped bool) {
	if maxMatches <= 0 {
		maxMatches = MaxMatches
	}
	if maxLines <= 0 {
		maxLines = MaxScanLines
	}
	if q.Empty() {
		return nil, false
	}
	if q.jq != nil {
		return q.structuralScan()
	}
	lines := b.LineCount()
	if lines > maxLines {
		lines, capped = maxLines, true
	}
	for i := 0; i < lines; i++ {
		for _, s := range q.LineMatches(b, i) {
			if len(spans) == maxMatches {
				return spans, true
			}
			spans = append(spans, s)
		}
	}
	return spans, capped
}

// IndexOf returns the 1-based position of the span starting at pos within
// spans, or 0 when pos sits on none of them.
func IndexOf(spans []Span, pos buffer.Position) int {
	for i, s := range spans {
		if s.Line == pos.Line && s.Start == pos.Col {
			return i + 1
		}
	}
	return 0
}

// CountMatches tallies q's matches against pos under the same budgets as
// ScanMatches. The scan runs in reading order from the buffer start, so the
// index is stable no matter which direction the search ran.
func (q Query) CountMatches(b *buffer.Buffer, pos buffer.Position, maxMatches, maxLines int) Tally {
	spans, capped := q.ScanMatches(b, maxMatches, maxLines)
	return Tally{Index: IndexOf(spans, pos), Total: len(spans), Capped: capped}
}

// runeCol converts a byte offset within line to a rune column.
func runeCol(line string, byteOff int) int {
	n := 0
	for i := range line {
		if i >= byteOff {
			break
		}
		n++
	}
	return n
}

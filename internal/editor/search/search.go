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
	"unicode/utf8"

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
	// multi marks a pattern that itself holds a line break and therefore
	// matches across line boundaries (#2600, multiline.go); breaks is how many
	// it holds, which bounds how far a match can reach.
	multi  bool
	breaks int
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
	if spansLines(pattern, regex) {
		// A pattern carrying a line break (#2600) leaves the per-line world
		// entirely — multiline.go matches it against joined lines.
		return compileMulti(q, insensitive)
	}
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
	if q.multi {
		// A pattern spanning lines can never be answered by one line (#2600);
		// the predicate's callers (the follow filter) are all single-line.
		return false
	}
	if q.re != nil {
		return q.re.MatchString(text)
	}
	return strings.Contains(text, q.Pattern)
}

// LineMatches returns every match on line i as rune-column spans. For a
// pattern spanning lines (#2600) those are the *pieces* the matches contribute
// to line i, which is what the highlighter wants; AllMatches below counts each
// match once instead.
func (q Query) LineMatches(b *buffer.Buffer, i int) []Span {
	if q.jq != nil {
		return q.structuralLineMatches(i)
	}
	if q.Empty() {
		return nil
	}
	if q.multi {
		return q.multiLineMatches(b, i)
	}
	return q.scanText(b.Line(i), i, 0)
}

// LongLineBytes is the line length past which the per-line paths cut the
// line instead of scanning all of it (#2734): a landing scans on from the
// departure column (Step), and the windowed highlight (LineMatchesIn) scans
// only the columns it renders. Anchors and word boundaries at a cut can
// answer differently from the whole line; on a line this long a bounded
// cost matters more.
const LongLineBytes = 4096

// LineMatchesIn returns the matches of line i for a consumer that only
// looks at rune columns [lo, hi): every match while the line is of ordinary
// length (LineMatches, exactly), and on a line longer than LongLineBytes
// only the matches found scanning that window — one bounded pass instead of
// the whole line, which the view asks for per visible line per frame.
func (q Query) LineMatchesIn(b *buffer.Buffer, i, lo, hi int) []Span {
	line := b.Line(i)
	if q.jq != nil || q.multi || q.Empty() || len(line) <= LongLineBytes {
		return q.LineMatches(b, i)
	}
	if lo < 0 {
		lo = 0
	}
	blo := byteOffset(line, lo)
	bhi := len(line)
	if hi >= 0 {
		bhi = blo + byteOffset(line[blo:], hi-lo)
	}
	if blo >= bhi {
		return nil
	}
	return q.scanText(line[blo:bhi], i, lo)
}

// scanText returns the matches in text as spans on line i, columns offset
// by base (the rune column text starts at).
func (q Query) scanText(text string, i, base int) []Span {
	var spans []Span
	// Byte offsets convert to rune columns through one forward walk over the
	// text (runeCounter) rather than a rescan from the start per match
	// (#2734): on a minified file with thousands of matches per multi-hundred-
	// kilobyte line the rescan made one line's matches quadratic — seconds
	// per keystroke — where they are linear now.
	rc := runeCounter{line: text}
	if q.re != nil {
		for _, m := range q.re.FindAllStringIndex(text, -1) {
			if m[0] == m[1] {
				continue // skip empty matches
			}
			spans = append(spans, Span{Line: i, Start: base + rc.col(m[0]), End: base + rc.col(m[1])})
		}
		return spans
	}
	from := 0
	for {
		idx := strings.Index(text[from:], q.Pattern)
		if idx < 0 {
			break
		}
		bs := from + idx
		spans = append(spans, Span{Line: i, Start: base + rc.col(bs), End: base + rc.col(bs+len(q.Pattern))})
		from = bs + len(q.Pattern)
	}
	return spans
}

// firstIn returns the byte range of the first non-empty match in text.
func (q Query) firstIn(text string) (bs, be int, ok bool) {
	if q.re == nil {
		idx := strings.Index(text, q.Pattern)
		if idx < 0 {
			return 0, 0, false
		}
		return idx, idx + len(q.Pattern), true
	}
	for off := 0; off <= len(text); {
		m := q.re.FindStringIndex(text[off:])
		if m == nil {
			return 0, 0, false
		}
		if m[0] != m[1] {
			return off + m[0], off + m[1], true
		}
		// An empty match: step one rune past it and look again.
		_, size := utf8.DecodeRuneInString(text[off+m[0]:])
		if size == 0 {
			size = 1
		}
		off += m[0] + size
	}
	return 0, 0, false
}

// byteOffset converts rune column col of line to its byte offset, clamped
// to the line end.
func byteOffset(line string, col int) int {
	off := 0
	for n := 0; n < col && off < len(line); n++ {
		_, size := utf8.DecodeRuneInString(line[off:])
		off += size
	}
	return off
}

// runeCounter converts ascending byte offsets of one line to rune columns in
// a single forward pass; an offset before the last one restarts the walk.
type runeCounter struct {
	line  string
	at    int // the byte offset the walk reached
	runes int // its rune column
}

func (c *runeCounter) col(byteOff int) int {
	if byteOff < c.at {
		c.at, c.runes = 0, 0
	}
	for c.at < byteOff && c.at < len(c.line) {
		_, size := utf8.DecodeRuneInString(c.line[c.at:])
		c.at += size
		c.runes++
	}
	return c.runes
}

// AllMatches returns every match in the buffer in reading order — one span per
// match, so a match spanning lines (#2600) is one entry, starting where it
// starts. That is what n/N stepping, the tally and the multi-caret need.
func (q Query) AllMatches(b *buffer.Buffer) []Span {
	if q.multi {
		return q.multiAll(b)
	}
	var out []Span
	for i := 0; i < b.LineCount(); i++ {
		out = append(out, q.LineMatches(b, i)...)
	}
	return out
}

// Next finds the count-th match from the cursor in dir, wrapping around the
// buffer ends. ok is false when the pattern matches nothing. It is the
// unbudgeted landing scan (landing.go, #2734): the walk stops at the match
// it lands on, so its cost is the distance to that match, not the buffer —
// but a pattern matching nowhere still costs the whole buffer, which is why
// the editor steps the scan under a budget instead of calling this.
func (q Query) Next(b *buffer.Buffer, from buffer.Position, dir Direction, count int) (buffer.Position, bool) {
	l := q.Step(b, q.Begin(from, dir, count), 0)
	if !l.Found {
		return from, false
	}
	return l.Pos, true
}

// Match-tally caps (#2145). A tally must not cost a full scan of a very large
// buffer on every keystroke of an incremental search, so counting stops once
// either budget runs out and the result is reported as capped ("999+").
const (
	// MaxMatches is the largest exact total a tally reports.
	MaxMatches = 999
	// MaxScanLines bounds how many buffer lines one tally scans.
	MaxScanLines = 20000
	// MaxScanBytes bounds the line text one tally scans (#2734): a file of a
	// few very long lines stays under the line budget while its bytes do
	// not, and the tally runs on the event loop.
	MaxScanBytes = 2 << 20
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
// MaxMatches / MaxScanLines defaults) — and never more than MaxScanBytes of
// line text. capped reports that a budget ran out, so the result is a prefix
// of the buffer's matches rather than all of them.
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
	lines, capped := scanExtent(b, maxLines, MaxScanBytes)
	if q.multi {
		return q.multiScan(b, maxMatches, lines, capped)
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

// scanExtent is how many leading lines a tally scans under the line and
// byte budgets, and whether either cut the buffer short.
func scanExtent(b *buffer.Buffer, maxLines, maxBytes int) (lines int, capped bool) {
	lines = b.LineCount()
	if lines > maxLines {
		lines, capped = maxLines, true
	}
	bytes := 0
	for i := 0; i < lines; i++ {
		if bytes += len(b.Line(i)) + 1; bytes > maxBytes && i > 0 {
			return i, true
		}
	}
	return lines, capped
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

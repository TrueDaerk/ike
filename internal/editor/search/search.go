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

// Span is a match on a single line, as rune columns [Start, End).
type Span struct {
	Line       int
	Start, End int
}

// Query is a compiled search request.
type Query struct {
	Pattern string
	Regex   bool
	re      *regexp.Regexp
}

// Compile builds a Query. When regex is true and the pattern is invalid, it
// falls back to a literal search so a half-typed regex never errors mid-keypress.
//
// Matching is smartcase (#257), vim-style: an all-lowercase pattern matches
// case-insensitively, any uppercase rune makes it exact. A case-insensitive
// literal runs through a quoted regex so multi-byte case pairs fold correctly;
// the exact literal keeps the strings.Index fast path.
func Compile(pattern string, regex bool) Query {
	q := Query{Pattern: pattern, Regex: regex}
	if pattern == "" {
		return q
	}
	insensitive := strings.IndexFunc(pattern, unicode.IsUpper) < 0
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

// LineMatches returns every match on line i as rune-column spans.
func (q Query) LineMatches(b *buffer.Buffer, i int) []Span {
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

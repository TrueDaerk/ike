package search

// multiline.go is the across-line-boundaries matching path (#2600): a pattern
// that itself contains a line break — typed with alt+enter in the "/" line or
// the find/replace panel — must match text that spans two buffer lines, which
// the per-line scan in search.go structurally cannot do.
//
// It is a second path, not a replacement: a Query without a break in its
// pattern keeps the line-by-line scan (and its strings.Index fast path)
// untouched, so the common search costs exactly what it did before.
//
// The public shape of a match is unchanged — Span is still a single line's
// rune columns — and the two consumers want two different things from a
// multi-line match, so they get two different answers:
//
//   - LineMatches serves the *highlighter*: it returns the pieces a match
//     contributes to one line, so a match spanning lines 4 and 5 paints on
//     both.
//   - AllMatches / ScanMatches serve n/N, the tally and the multi-caret: they
//     return one head span per match (its start line and start column), so a
//     two-line match counts once and n steps onto it once.
//
// LineMatches cannot afford a whole-buffer scan — the view asks it per visible
// line per frame — so it scans a window around the line instead: a pattern
// with k breaks can only reach a line from at most k lines above it. Because
// leftmost-first scanning makes a match's boundary depend on the text before
// it, a window whose own first lines already hold a match is widened and
// rescanned until the boundary settles (or maxLookback lines are spent), so
// the highlight agrees with the tally.

import (
	"regexp"
	"sort"
	"strings"

	"ike/internal/editor/buffer"
)

// maxLookback bounds how far back multiLineMatches widens its window chasing a
// stable match boundary. Past it the window's answer stands: a pattern whose
// matches chain over hundreds of lines is pathological, and a bounded frame
// cost matters more than the last pixel of highlight fidelity.
const maxLookback = 256

// mrange is one multi-line match, as buffer positions.
type mrange struct{ start, end buffer.Position }

// spansLines reports whether a pattern reaches across a line boundary: it holds
// a real break (what alt+enter types into the search line), or — as a regex —
// writes one as the `\n` / `\r` escape, which is how the same break survives
// the find/replace panel's single-line ex hand-off.
func spansLines(pattern string, regex bool) bool {
	return strings.Contains(pattern, "\n") || regex && breakEscapes(pattern) > 0
}

// patternBreaks counts the line breaks a pattern holds, which bounds how many
// lines one of its matches can reach over.
func patternBreaks(pattern string, regex bool) int {
	n := strings.Count(pattern, "\n")
	if regex {
		n += breakEscapes(pattern)
	}
	return n
}

// breakEscapes counts the `\n` / `\r` escapes in a regex source, stepping over
// `\\` so an escaped backslash never reads as the start of one.
func breakEscapes(expr string) int {
	n := 0
	for i := 0; i < len(expr); i++ {
		if expr[i] != '\\' || i+1 >= len(expr) {
			continue
		}
		if expr[i+1] == 'n' || expr[i+1] == 'r' {
			n++
		}
		i++
	}
	return n
}

// compileMulti finishes a Query whose pattern spans lines. The expression is
// compiled with (?m), so "^" and "$" keep meaning line start / line end the way
// they do in the per-line path — and so a windowed rescan can never disagree
// with a whole-buffer one about them. "." keeps *not* matching a newline: a
// break in the pattern is written, not stumbled into.
func compileMulti(q Query, insensitive bool) Query {
	q.multi = true
	q.breaks = patternBreaks(q.Pattern, q.Regex)
	flags := "(?m)"
	if insensitive {
		flags = "(?mi)"
	}
	expr := q.Pattern
	if !q.Regex {
		expr = regexp.QuoteMeta(expr)
	}
	re, err := regexp.Compile(flags + expr)
	if err != nil {
		// Half-typed regex: fall back to the literal spelling, exactly like the
		// single-line path, so a search never errors mid-keypress.
		q.Regex = false
		re = regexp.MustCompile(flags + regexp.QuoteMeta(q.Pattern))
	}
	q.re = re
	return q
}

// multiLineMatches returns the pieces every multi-line match contributes to
// line i — what the view highlights.
func (q Query) multiLineMatches(b *buffer.Buffer, i int) []Span {
	back := q.breaks
	if back < 1 {
		back = 1
	}
	from, to := i-back, i+back
	limit := i - maxLookback
	for {
		if from < 0 {
			from = 0
		}
		ms := q.scanLines(b, from, to)
		if from == 0 || from <= limit || !anchoredNear(ms, from, back) {
			return piecesOn(b, ms, i)
		}
		from -= back
	}
}

// anchoredNear reports whether a match begins within the first back lines of a
// window starting at from — the zone where text before the window could still
// have moved the boundary, and therefore the signal to widen and rescan.
func anchoredNear(ms []mrange, from, back int) bool {
	for _, mr := range ms {
		if mr.start.Line < from+back {
			return true
		}
	}
	return false
}

// piecesOn collects the spans the matches contribute to line i.
func piecesOn(b *buffer.Buffer, ms []mrange, i int) []Span {
	var out []Span
	for _, mr := range ms {
		if mr.start.Line > i || mr.end.Line < i {
			continue
		}
		for _, s := range pieces(b, mr) {
			if s.Line == i {
				out = append(out, s)
			}
		}
	}
	return out
}

// pieces splits one match into the per-line spans that render it: the tail of
// its first line, whole lines in between, the head of its last line.
func pieces(b *buffer.Buffer, mr mrange) []Span {
	if mr.start.Line == mr.end.Line {
		return []Span{{Line: mr.start.Line, Start: mr.start.Col, End: mr.end.Col}}
	}
	out := []Span{{Line: mr.start.Line, Start: mr.start.Col, End: b.RuneLen(mr.start.Line)}}
	for l := mr.start.Line + 1; l < mr.end.Line; l++ {
		out = append(out, Span{Line: l, Start: 0, End: b.RuneLen(l)})
	}
	if mr.end.Col > 0 {
		out = append(out, Span{Line: mr.end.Line, Start: 0, End: mr.end.Col})
	}
	return out
}

// head is the one span that stands for a whole match in the match *list*: its
// start line, from the start column to wherever the match leaves that line.
func head(b *buffer.Buffer, mr mrange) Span {
	end := mr.end.Col
	if mr.end.Line != mr.start.Line {
		end = b.RuneLen(mr.start.Line)
	}
	return Span{Line: mr.start.Line, Start: mr.start.Col, End: end}
}

// multiAll returns one head span per match over the whole buffer.
func (q Query) multiAll(b *buffer.Buffer) []Span {
	var out []Span
	for _, mr := range q.scanLines(b, 0, b.LineCount()-1) {
		out = append(out, head(b, mr))
	}
	return out
}

// multiScan serves ScanMatches under the same budgets as the per-line path:
// the first lines buffer lines (capped reporting that a budget cut the
// buffer short) and at most maxMatches matches.
func (q Query) multiScan(b *buffer.Buffer, maxMatches, lines int, capped bool) (spans []Span, _ bool) {
	last := lines - 1
	for _, mr := range q.scanLines(b, 0, last) {
		if len(spans) == maxMatches {
			return spans, true
		}
		spans = append(spans, head(b, mr))
	}
	return spans, capped
}

// scanLines runs the multi-line expression over lines [from, to] joined with
// "\n" and returns the matches as buffer position ranges. Zero-width matches
// are skipped, mirroring the per-line scan.
func (q Query) scanLines(b *buffer.Buffer, from, to int) []mrange {
	if from < 0 {
		from = 0
	}
	if n := b.LineCount(); to >= n {
		to = n - 1
	}
	if q.re == nil || from > to {
		return nil
	}
	lines := make([]string, 0, to-from+1)
	for i := from; i <= to; i++ {
		lines = append(lines, b.Line(i))
	}
	text := strings.Join(lines, "\n")
	locs := q.re.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return nil
	}
	off := newOffsets(lines, from)
	out := make([]mrange, 0, len(locs))
	for _, l := range locs {
		if l[0] == l[1] {
			continue
		}
		out = append(out, mrange{start: off.pos(l[0]), end: off.pos(l[1])})
	}
	return out
}

// offsets maps a byte offset inside the joined window text back to a buffer
// position.
type offsets struct {
	first  int // buffer line index of lines[0]
	lines  []string
	starts []int // byte offset of each line inside the joined text
}

func newOffsets(lines []string, first int) offsets {
	starts := make([]int, len(lines))
	n := 0
	for i, l := range lines {
		starts[i] = n
		n += len(l) + 1 // + the joining "\n"
	}
	return offsets{first: first, lines: lines, starts: starts}
}

// pos resolves a byte offset. An offset landing on a joining newline belongs to
// the line before it, at its end — which is exactly where a match that runs to
// the end of a line should report.
func (o offsets) pos(byteOff int) buffer.Position {
	i := sort.Search(len(o.starts), func(k int) bool { return o.starts[k] > byteOff }) - 1
	if i < 0 {
		i = 0
	}
	return buffer.Position{Line: o.first + i, Col: runeCol(o.lines[i], byteOff-o.starts[i])}
}

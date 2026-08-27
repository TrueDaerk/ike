package editor

// logfilter.go is the live filter/highlight over a followed buffer (#2255).
// A busy log stream cannot be narrowed by scrolling: follow mode (follow.go)
// keeps appending, and the interesting lines drown. The filter line —
// view.followFilter ("|" prompt), view.followHighlight ("*" prompt) — narrows
// or marks the tail as it arrives:
//
//   - Filter mode hides every non-matching line, existing and newly appended
//     alike, and the auto-scroll sticks to the *filtered* tail.
//   - Highlight mode hides nothing and colours the matches instead.
//
// The filter is a *view* concern, like folding: the buffer keeps every line,
// so clearing the pattern restores the whole stream, and a shared view of the
// same document filters independently. Hiding rides the fold machinery
// (fold.go): a filtered-out line is hidden for motions, scrolling, mouse
// mapping and the render loop alike, exactly like a collapsed fold body.
//
// The pattern language is the search line's (#255, #1111): a plain substring
// by default, "\v" switching to a regex, "\c"/"\C" forcing the case mode over
// smartcase. Unlike the search line, a broken regex is *not* silently demoted
// to a literal — a filter that quietly matches something else would hide the
// wrong lines — it reports inline and leaves the stream unfiltered.
//
// Everything is gated on follow mode: leaving it drops the filter, so the
// buffer never stays mysteriously narrowed after the tail stopped.

import (
	"regexp"
	"strconv"

	"ike/internal/editor/buffer"
	"ike/internal/editor/search"
)

// logFilter is one view's follow filter. An empty query means no filter;
// hilite marks highlight-only mode (nothing hidden), and err carries the
// inline message of a pattern that failed to compile — with an error set the
// filter matches nothing and hides nothing.
type logFilter struct {
	q      search.Query
	line   string // the typed line, markers included, for the badge
	hilite bool
	err    string
}

// active reports whether the filter has something to apply.
func (f logFilter) active() bool { return f.err == "" && !f.q.Empty() }

// logFilterState caches the number of matching lines of one document version
// for the badge (#2255). Recounting a tailed log per append is exactly the
// O(document)-per-poll work #2163 removed from the append path, so the count
// extends incrementally instead: count covers the lines [0, appendFrom) when
// appendFrom > 0, and the extension scans only the tail from there.
//
// A pointer field on Model, like logRunCache, so the value copies each Update
// returns share it.
type logFilterState struct {
	valid   bool
	version int
	path    string
	key     string // the query's identity (search.Query.ID)
	count   int
	// appendFrom is the first line the count does not cover yet (0: it covers
	// everything up to version), appendVersion the document version the
	// appends produced.
	appendFrom    int
	appendVersion int
}

// FollowFiltering reports whether this view narrows its tail (#2255) — the
// hiding mode of the follow filter, as opposed to highlight-only.
func (m Model) FollowFiltering() bool { return m.logFilterHiding() }

// FollowFilterLabel is the status-line badge next to the follow badge: "" with
// no filter, otherwise the mode, the pattern and how many lines it matches —
// or the inline error of a pattern that would not compile.
func (m Model) FollowFilterLabel() string {
	f := m.logFilt
	if !m.follow || (f.line == "" && f.err == "") {
		return ""
	}
	kind := "FILTER"
	if f.hilite {
		kind = "HIGHLIGHT"
	}
	pat := f.q.Pattern
	if pat == "" {
		pat = f.line
	}
	if f.q.Regex {
		pat = "~" + pat
	}
	if f.err != "" {
		return kind + " " + pat + ": " + f.err
	}
	if !f.active() {
		return ""
	}
	n := m.logFilterCount()
	if n == 0 {
		return kind + " " + pat + " (no matches)"
	}
	return kind + " " + pat + " (" + strconv.Itoa(n) + ")"
}

// logFilterHiding reports whether the filter currently hides lines: an active
// pattern in filter (not highlight) mode on a following view.
func (m Model) logFilterHiding() bool {
	return m.follow && !m.logFilt.hilite && m.logFilt.active()
}

// logFilterHidden reports whether line is filtered out of the view. It is
// asked per rendered row per frame (lineHidden), so it stays a single
// substring/regex match on the line — no allocation, no whole-buffer scan.
func (m Model) logFilterHidden(line int) bool {
	if !m.logFilterHiding() {
		return false
	}
	return !m.logFilt.q.MatchesLine(m.buf.Line(line))
}

// logFilterSpans returns the filter's matches on one line, the highlight half
// of the feature. Filter mode marks its matches too: the surviving lines still
// gain from seeing *why* they survived.
func (m Model) logFilterSpans(line int) []search.Span {
	if !m.follow || !m.logFilt.active() {
		return nil
	}
	return m.logFilt.q.LineMatches(m.buf, line)
}

// logFilterLastLine is the last line the filtered view shows — where the
// auto-scroll sticks while following. Without a filter that is the buffer's
// last line; with one, the last matching line (0 when nothing matches).
func (m Model) logFilterLastLine() int {
	last := m.buf.LineCount() - 1
	if !m.logFilterHiding() {
		return last
	}
	for last > 0 && m.logFilterHidden(last) {
		last--
	}
	return last
}

// logFilterCount is the number of lines the pattern matches, cached per
// document version and extended incrementally over follow appends.
func (m Model) logFilterCount() int {
	st := m.logFiltCache
	if !m.logFilt.active() {
		return 0
	}
	key := m.logFilt.q.ID()
	if st == nil {
		return m.countFilterMatches(0, m.buf.LineCount())
	}
	if st.valid && st.path == m.path && st.key == key {
		switch {
		case st.appendFrom == 0 && st.version == m.docVersion:
			return st.count
		case st.appendFrom > 0 && st.appendVersion == m.docVersion:
			// Only follow-mode appends happened since the count: extend it
			// over the new tail instead of rescanning everything.
			st.count += m.countFilterMatches(st.appendFrom, m.buf.LineCount())
			st.version, st.appendFrom, st.appendVersion = m.docVersion, 0, 0
			return st.count
		}
	}
	st.valid, st.version, st.path, st.key = true, m.docVersion, m.path, key
	st.appendFrom, st.appendVersion = 0, 0
	st.count = m.countFilterMatches(0, m.buf.LineCount())
	return st.count
}

// countFilterMatches counts the matching lines in [from, to).
func (m Model) countFilterMatches(from, to int) int {
	n := 0
	for i := from; i < to; i++ {
		if m.logFilt.q.MatchesLine(m.buf.Line(i)) {
			n++
		}
	}
	return n
}

// noteLogFilterAppend registers a follow-mode append with the match-count
// cache, mirroring noteLogAppend (logfold.go): prevCount is the line count
// before the append, merged whether the previous last line was continued in
// place. A merged line the count already covered loses its old contribution
// here — its text is about to change — and the uncounted region starts at it.
//
// Called *before* the buffer mutation, unlike noteLogAppend: the pre-merge
// match state of the continued line is unknowable afterwards.
func (m *Model) noteLogFilterAppend(prevCount int, merged bool) {
	st := m.logFiltCache
	if st == nil || !st.valid || st.path != m.path || !m.logFilt.active() ||
		st.key != m.logFilt.q.ID() {
		return
	}
	from := prevCount
	if merged {
		if st.appendFrom == 0 {
			// The count covers the line that is about to change: drop its
			// contribution, the extension recounts it.
			if m.logFilt.q.MatchesLine(m.buf.Line(prevCount - 1)) {
				st.count--
			}
			from = prevCount - 1
		} else {
			from = st.appendFrom // already uncounted
		}
	}
	if st.appendFrom == 0 || from < st.appendFrom {
		st.appendFrom = from
	}
}

// noteLogFilterVersion stamps the document version an append produced onto the
// count cache. Split from noteLogFilterAppend because the version only moves
// with the append's EventChange, after the buffer mutation.
func (m *Model) noteLogFilterVersion() {
	if st := m.logFiltCache; st != nil && st.valid && st.appendFrom > 0 {
		st.appendVersion = m.docVersion
	}
}

// beginFollowFilter opens the filter line on the command line, in hiding mode
// or highlight-only mode. The current filter is captured so Esc restores it,
// and the line starts prefilled with the active pattern — reopening it is how
// a pattern gets edited.
func (m *Model) beginFollowFilter(hilite bool) {
	if !m.follow {
		m.cmdMsg = "E: filter needs follow mode (view.toggleFollow)"
		return
	}
	m.collapseCarets()
	m.filtPrev = m.logFilt
	m.mode = Command
	m.filtering = true
	m.cmdline = m.logFilt.line
	m.cmdCur = len([]rune(m.cmdline))
	m.cmdSelStart, m.cmdSelEnd = 0, m.cmdCur
	m.cmdHistIdx = -1
	m.applyFollowFilter(m.cmdline, hilite)
}

// followFilterPreview re-applies the half-typed pattern, so the stream
// narrows (or lights up) live while typing — the filter's incsearch.
func (m *Model) followFilterPreview() {
	m.applyFollowFilter(m.cmdline, m.logFilt.hilite)
}

// cancelFollowFilter abandons the filter line, restoring the filter that was
// active when it opened.
func (m *Model) cancelFollowFilter() {
	m.logFilt = m.filtPrev
	m.filtPrev = logFilter{}
	m.filtering = false
	m.afterFilterChange()
}

// commitFollowFilter closes the filter line on the pattern as typed. An empty
// pattern clears the filter, which is how everything comes back.
func (m *Model) commitFollowFilter() {
	m.applyFollowFilter(m.cmdline, m.logFilt.hilite)
	m.filtPrev = logFilter{}
	m.filtering = false
	switch {
	case m.logFilt.err != "":
		m.cmdMsg = "E: " + m.logFilt.err
	case m.logFilt.q.Empty():
		m.cmdMsg = "filter cleared"
	}
}

// clearFollowFilter drops the filter outright (view.clearFollowFilter), the
// one-key way back to the unfiltered stream.
func (m *Model) clearFollowFilter() {
	if m.logFilt.line == "" && m.logFilt.err == "" {
		return
	}
	m.logFilt = logFilter{}
	m.afterFilterChange()
	m.cmdMsg = "filter cleared"
}

// applyFollowFilter compiles the typed line into the view's filter and
// re-frames the view around it.
//
// A regex that does not compile is reported rather than demoted: search.Compile
// falls back to a literal for a half-typed pattern (harmless when it only moves
// the cursor), but a filter that silently matches a different thing would hide
// the wrong half of the log.
func (m *Model) applyFollowFilter(line string, hilite bool) {
	f := logFilter{line: line, hilite: hilite}
	pattern, rx, cs := m.parseSearchPattern(line)
	switch {
	case pattern == "":
		// Nothing to compile: the empty filter shows the whole stream.
	case rx:
		if _, err := regexp.Compile(pattern); err != nil {
			f.err = "bad regex: " + regexErrText(err)
		} else {
			f.q = search.Compile(pattern, true, cs)
		}
	default:
		f.q = search.Compile(pattern, false, cs)
	}
	m.logFilt = f
	m.afterFilterChange()
}

// afterFilterChange re-frames the view after the visible set changed: the
// match count is stale, hidden rows shift every row below them, and a cursor
// left on a filtered-out line would be invisible. While the auto-scroll is
// live the view re-sticks to the filtered tail; a paused view keeps its place
// and only lifts the cursor onto a visible line.
func (m *Model) afterFilterChange() {
	if st := m.logFiltCache; st != nil {
		st.valid = false
	}
	m.bumpRender()
	m.bumpFolds()
	if !m.follow {
		return
	}
	if !m.followPaused {
		m.followToEnd()
		return
	}
	m.snapCursorVisible()
	m.scroll()
}

// snapCursorVisible moves a cursor stranded on a filtered-out line onto the
// nearest visible one — downwards first (the reading direction of a log),
// upwards when nothing below matches.
func (m *Model) snapCursorVisible() {
	if !m.logFilterHiding() || !m.logFilterHidden(m.cursor.Line) {
		return
	}
	lc := m.buf.LineCount()
	for n := m.cursor.Line + 1; n < lc; n++ {
		if !m.logFilterHidden(n) {
			m.moveTo(buffer.Position{Line: n, Col: 0})
			return
		}
	}
	for n := m.cursor.Line - 1; n >= 0; n-- {
		if !m.logFilterHidden(n) {
			m.moveTo(buffer.Position{Line: n, Col: 0})
			return
		}
	}
}

// regexErrText trims the "error parsing regexp: " prefix off a compile error,
// which would push the actual complaint off a narrow status line.
func regexErrText(err error) string {
	const prefix = "error parsing regexp: "
	s := err.Error()
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

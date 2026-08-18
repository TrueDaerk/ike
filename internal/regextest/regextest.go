// Package regextest is the evaluation core of the regex tester (#1937):
// compile a pattern, run it over a block of test text, and report what
// matched — with the capture groups of every match, the line/column spans the
// host highlights, and the quoted forms of the pattern for pasting into Go
// code or into the config files that embed regexes (problem matchers).
//
// It holds no UI state on purpose: the floating tester in internal/app owns
// the fields, the cursor and the rendering, and calls Evaluate on every
// keystroke. Everything here is pure, so the interesting behavior — group
// extraction, named groups, invalid patterns, span mapping — is testable
// without a terminal.
//
// Semantics are Go's regexp (RE2), not PCRE: no backreferences, no
// lookaround, linear time in the input. The tester says so on screen, because
// a user arriving from JetBrains or from a PCRE tool would otherwise read a
// "missing argument to repetition operator" error as a bug. Linear time is
// also why evaluation is safe to run at all: there is no catastrophic
// backtracking to freeze the UI. The host still evaluates large texts off the
// event loop (see AsyncThreshold) so a big paste cannot stall a keystroke.
package regextest

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxMatches caps how many matches Evaluate collects. A pattern like `.?`
// over a large paste matches once per character; the tester only ever shows a
// count and a highlighted excerpt, so the cap bounds the work without hiding
// anything the user could have read. Result.Truncated marks a capped scan.
const MaxMatches = 5000

// AsyncThreshold is the test-text size (in bytes) above which the host runs
// Evaluate off the event loop instead of inline in the key handler. RE2 is
// linear, so this is about the constant factor on a megabyte paste, not about
// pathological patterns.
const AsyncThreshold = 16 << 10

// Group is one capture group of a match. Index 0 is the whole match; Name is
// empty for unnamed groups. Set is false for a group that did not participate
// in the match — `(a)|(b)` always leaves one of the two unset — which is a
// different thing from a group that matched the empty string.
type Group struct {
	Index int
	Name  string
	Start int // byte offset into the test text, -1 when unset
	End   int
	Value string
	Set   bool
}

// Match is one match of the pattern with its groups (Groups[0] is the whole
// match, so the slice is never empty).
type Match struct {
	Index  int // 0-based position in the match list
	Start  int // byte offsets into the test text
	End    int
	Value  string
	Groups []Group
}

// Result is one evaluation of a pattern against a text.
type Result struct {
	// Err is the compile error message, empty when the pattern compiled. A
	// failed compile carries no matches.
	Err string
	// Matches is every match, capped at MaxMatches.
	Matches []Match
	// Truncated reports that the scan hit MaxMatches and stopped.
	Truncated bool
}

// Evaluate compiles pattern and collects its matches in text. An empty
// pattern is idle, not an error: it compiles fine and would match the empty
// string everywhere, which is noise while the user is still typing.
func Evaluate(pattern, text string) Result {
	if pattern == "" {
		return Result{}
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return Result{Err: compileError(err)}
	}
	// One over the cap, so a scan that fills the cap is distinguishable from
	// one that happened to end there.
	found := re.FindAllStringSubmatchIndex(text, MaxMatches+1)
	res := Result{}
	if len(found) > MaxMatches {
		found, res.Truncated = found[:MaxMatches], true
	}
	names := re.SubexpNames()
	for i, idx := range found {
		res.Matches = append(res.Matches, buildMatch(i, idx, names, text))
	}
	return res
}

// buildMatch turns one FindAllStringSubmatchIndex entry into a Match.
func buildMatch(i int, idx []int, names []string, text string) Match {
	m := Match{Index: i, Start: idx[0], End: idx[1], Value: text[idx[0]:idx[1]]}
	for g := 0; g*2 < len(idx); g++ {
		start, end := idx[g*2], idx[g*2+1]
		grp := Group{Index: g, Start: start, End: end}
		if g < len(names) {
			grp.Name = names[g]
		}
		if start >= 0 && end >= start {
			grp.Set, grp.Value = true, text[start:end]
		}
		m.Groups = append(m.Groups, grp)
	}
	return m
}

// compileError trims regexp's "error parsing regexp: " preamble — the tester
// already labels the line as the pattern error, so the prefix only eats width.
func compileError(err error) string {
	msg := err.Error()
	return strings.TrimPrefix(msg, "error parsing regexp: ")
}

// Span is a highlighted run on one line of the test text, in rune columns:
// [Start,End) on line Line belongs to match Match. A match crossing a line
// break yields one span per line it covers.
type Span struct {
	Line  int
	Start int
	End   int
	Match int
}

// LineSpans maps matches (byte offsets) onto per-line rune columns so the
// renderer can highlight without knowing about offsets. Empty matches produce
// no span — there is nothing to color — but they still count as matches.
func LineSpans(text string, matches []Match) []Span {
	if len(matches) == 0 {
		return nil
	}
	starts, cols := lineIndex(text)
	var spans []Span
	for _, m := range matches {
		if m.End <= m.Start {
			continue
		}
		line := lineAt(starts, m.Start)
		for line < len(starts) && starts[line] < m.End {
			lineStart := starts[line]
			lineEnd := lineStart + len(cols[line])
			from, to := max(m.Start, lineStart), min(m.End, lineEnd)
			if to > from {
				spans = append(spans, Span{
					Line:  line,
					Start: utf8.RuneCountInString(text[lineStart:from]),
					End:   utf8.RuneCountInString(text[lineStart:to]),
					Match: m.Index,
				})
			}
			line++
		}
	}
	return spans
}

// lineIndex returns the byte offset of every line start plus the lines
// themselves (without their terminator), so callers can map an offset to a
// line and a rune column.
func lineIndex(text string) (starts []int, lines []string) {
	lines = strings.Split(text, "\n")
	off := 0
	for _, l := range lines {
		starts = append(starts, off)
		off += len(l) + 1 // + the "\n" that Split removed
	}
	return starts, lines
}

// lineAt finds the line holding byte offset off (binary search would be
// overkill: the tester's text is a screenful, and matches arrive in order).
func lineAt(starts []int, off int) int {
	for i := len(starts) - 1; i >= 0; i-- {
		if starts[i] <= off {
			return i
		}
	}
	return 0
}

// QuoteFormat names one way of embedding a pattern in source or config.
type QuoteFormat int

const (
	// QuoteGoRaw is a Go raw string literal — the form regexp patterns want,
	// since backslashes stay literal. Falls back to QuoteGo when the pattern
	// cannot live in backticks.
	QuoteGoRaw QuoteFormat = iota
	// QuoteGo is a Go interpreted string literal (backslashes doubled).
	QuoteGo
	// QuoteTOML is a TOML literal string ('…') — the form IKE's config takes
	// for the regexes it embeds, e.g. a [[tasks.matcher]] rule. Falls back to
	// a TOML basic string when the pattern contains a single quote.
	QuoteTOML
	// QuoteJSON is a JSON string, for the editor/task config formats that
	// embed regexes as JSON (VS Code-style problem matchers).
	QuoteJSON
)

// QuoteFormats is the cycle order offered by the tester.
var QuoteFormats = []QuoteFormat{QuoteGoRaw, QuoteGo, QuoteTOML, QuoteJSON}

// String labels the format for the tester's copy hint.
func (f QuoteFormat) String() string {
	switch f {
	case QuoteGoRaw:
		return "Go raw"
	case QuoteGo:
		return "Go"
	case QuoteTOML:
		return "TOML"
	case QuoteJSON:
		return "JSON"
	}
	return "Go raw"
}

// Quote renders pattern as a literal in the given format.
func Quote(pattern string, f QuoteFormat) string {
	switch f {
	case QuoteGoRaw:
		if strconv.CanBackquote(pattern) {
			return "`" + pattern + "`"
		}
		return strconv.Quote(pattern) // backtick or control rune inside
	case QuoteTOML:
		if !strings.ContainsAny(pattern, "'\n\r") {
			return "'" + pattern + "'"
		}
		return strconv.Quote(pattern) // TOML basic string, same escapes
	case QuoteJSON:
		b, err := json.Marshal(pattern)
		if err != nil {
			return strconv.Quote(pattern)
		}
		return string(b)
	}
	return strconv.Quote(pattern)
}

// HistoryLimit caps the per-session pattern history.
const HistoryLimit = 50

// History is the session-scoped list of patterns the tester evaluated,
// newest first. It lives in memory only: a regex under construction is
// scratch work, not something to persist into the project.
type History struct{ items []string }

// Add records pattern as the newest entry, moving a repeat to the front
// instead of duplicating it. Empty patterns are ignored.
func (h *History) Add(pattern string) {
	if pattern == "" {
		return
	}
	for i, p := range h.items {
		if p == pattern {
			h.items = append(h.items[:i], h.items[i+1:]...)
			break
		}
	}
	h.items = append([]string{pattern}, h.items...)
	if len(h.items) > HistoryLimit {
		h.items = h.items[:HistoryLimit]
	}
}

// Len reports how many patterns are remembered.
func (h *History) Len() int { return len(h.items) }

// At returns the i-th newest pattern; ok is false when i is out of range.
func (h *History) At(i int) (string, bool) {
	if i < 0 || i >= len(h.items) {
		return "", false
	}
	return h.items[i], true
}

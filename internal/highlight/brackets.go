package highlight

import (
	"strings"

	"ike/internal/bracket"
	"ike/internal/lang"
)

// brackets.go turns the buffer-level bracket pairing of internal/bracket into
// highlight spans (#1628): each bracket gets the rainbow capture of its
// nesting depth, so both halves of a pair render in one colour, and a bracket
// with no partner gets bracket.Unmatched, which the renderer styles as an
// error.
//
// Pairing used to be a walk over the Tree-sitter tree (#789). It moved here so
// that the depth a bracket renders with, the depth an indent guide renders
// with and the "is this bracket matched" answer all come from one pure-Go
// tracker — a tree walk can report where the brackets are, but not which of
// them has no partner. The grammar still decides what a string or a comment
// is: its own string/comment captures become the mask the scanner skips.

// maskCaptures are the capture heads whose spans hide their content from the
// bracket scanner: a `)` inside a string literal or a comment closes nothing.
var maskCaptures = []string{"string", "comment", "char"}

// proseLangs are the languages that are not scanned at all: their brackets are
// punctuation in a sentence, not structure. A ":-)" or a "(see below" in a
// paragraph is not a mistake, and marking it as one would make prose unreadable
// — the very files where a reader wants the least chrome.
var proseLangs = map[string]bool{
	"markdown": true, "markdown_inline": true, "text": true,
	"log": true, "csv": true, "tsv": true,
}

// bracketSpans scans lines for bracket pairs and returns one span per bracket,
// or nil when rainbow brackets are off. parsed are the spans the grammar
// produced for the same lines; when it produced any, they supply the exact
// string/comment mask, otherwise the scan falls back to the language's own
// comment and quote syntax.
func bracketSpans(l lang.Language, lines []string, parsed []Span) []Span {
	if !RainbowEnabled() || proseLangs[l.ID] {
		return nil
	}
	syn := bracket.Syntax{LineComment: l.LineComment, BlockComment: l.BlockComment}
	if len(parsed) > 0 {
		syn.Skip = maskOf(parsed)
	}
	marks := bracket.Scan(lines, syn)
	if len(marks) == 0 {
		return nil
	}
	spans := make([]Span, 0, len(marks))
	for _, m := range marks {
		capture := RainbowCapture(m.Depth)
		if m.Unmatched {
			capture = bracket.Unmatched
		}
		spans = append(spans, Span{Line: m.Line, StartCol: m.Col, EndCol: m.Col + 1, Capture: capture})
	}
	return spans
}

// maskOf builds the "inside a string or comment" predicate from parsed spans.
// Spans are grouped per line once; the per-cell lookup then walks only the
// masked runs of that line (a handful, even in string-heavy code).
func maskOf(spans []Span) func(line, col int) bool {
	byLine := make(map[int][]Span)
	for _, s := range spans {
		if !masked(s.Capture) {
			continue
		}
		byLine[s.Line] = append(byLine[s.Line], s)
	}
	if len(byLine) == 0 {
		return func(int, int) bool { return false }
	}
	return func(line, col int) bool {
		for _, s := range byLine[line] {
			if col >= s.StartCol && col < s.EndCol {
				return true
			}
		}
		return false
	}
}

// masked reports whether a capture hides its span from the bracket scanner.
// Sub-captures follow their head ("string.escape" is still a string), matching
// how the theme resolves capture names.
func masked(capture string) bool {
	for _, m := range maskCaptures {
		if capture == m || strings.HasPrefix(capture, m+".") {
			return true
		}
	}
	return false
}

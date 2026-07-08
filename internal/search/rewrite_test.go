package search

import "testing"

func TestRewriteRangeLiteral(t *testing.T) {
	got, ok := RewriteRange("foo needle bar", 4, 10, Query{Pattern: "needle"}, "thread")
	if !ok || got != "foo thread bar" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestRewriteRangeLiteralIgnoresDollarSigns(t *testing.T) {
	got, ok := RewriteRange("price needle", 6, 12, Query{Pattern: "needle"}, "$1 cash")
	if !ok || got != "price $1 cash" {
		t.Fatalf("literal replacement must not expand groups: %q", got)
	}
}

func TestRewriteRangeCaptureGroups(t *testing.T) {
	q := Query{Pattern: `needle(\d+)`, Regex: true}
	got, ok := RewriteRange("x needle42 y", 2, 10, q, "pin$1")
	if !ok || got != "x pin42 y" {
		t.Fatalf("capture group expansion failed: %q ok=%v", got, ok)
	}
}

func TestRewriteRangeNamedAndBracedGroups(t *testing.T) {
	q := Query{Pattern: `(?P<word>\w+)-(\d+)`, Regex: true}
	got, ok := RewriteRange("id: abc-7", 4, 9, q, "${2}:${word}")
	if !ok || got != "id: 7:abc" {
		t.Fatalf("braced group expansion failed: %q ok=%v", got, ok)
	}
}

func TestRewriteRangeStaleRegexRefused(t *testing.T) {
	q := Query{Pattern: `\d+`, Regex: true}
	// The range no longer holds digits: refuse rather than corrupt.
	got, ok := RewriteRange("now letters", 4, 11, q, "N")
	if ok {
		t.Fatalf("stale regex range must be refused, got %q", got)
	}
}

func TestRewriteRangeWholeWordKeepsGroupNumbers(t *testing.T) {
	// WholeWord wraps the pattern in \b(?:...)\b — non-capturing, so $1 still
	// refers to the user's first group.
	q := Query{Pattern: `(ne+dle)`, Regex: true, WholeWord: true}
	got, ok := RewriteRange("a needle b", 2, 8, q, "<$1>")
	if !ok || got != "a <needle> b" {
		t.Fatalf("whole-word wrapping broke group numbering: %q ok=%v", got, ok)
	}
}

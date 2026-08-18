package regextest

import (
	"strings"
	"testing"
)

// regextest_test.go covers the tester's evaluation core (#1937): match and
// group extraction, named groups, invalid patterns, span mapping, quoting and
// the session history.

// TestEvaluateMatchesAndGroups: every match carries its offsets, its text and
// its groups, with group 0 the whole match.
func TestEvaluateMatchesAndGroups(t *testing.T) {
	res := Evaluate(`(\w+)@(\w+)\.com`, "mail a@b.com and c@d.com here")
	if res.Err != "" {
		t.Fatalf("Evaluate() error = %q, want none", res.Err)
	}
	if len(res.Matches) != 2 {
		t.Fatalf("Evaluate() = %d matches, want 2", len(res.Matches))
	}
	first := res.Matches[0]
	if first.Value != "a@b.com" || first.Start != 5 || first.End != 12 {
		t.Errorf("first match = %q [%d,%d), want %q [5,12)", first.Value, first.Start, first.End, "a@b.com")
	}
	if len(first.Groups) != 3 {
		t.Fatalf("first match has %d groups, want 3 (whole + 2)", len(first.Groups))
	}
	if first.Groups[0].Value != "a@b.com" {
		t.Errorf("group 0 = %q, want the whole match", first.Groups[0].Value)
	}
	if first.Groups[1].Value != "a" || first.Groups[2].Value != "b" {
		t.Errorf("groups = %q/%q, want a/b", first.Groups[1].Value, first.Groups[2].Value)
	}
	if second := res.Matches[1]; second.Index != 1 || second.Value != "c@d.com" {
		t.Errorf("second match = #%d %q, want #1 %q", second.Index, second.Value, "c@d.com")
	}
}

// TestEvaluateNamedGroups: named groups report their name; unnamed ones stay
// empty, and group 0 is never named.
func TestEvaluateNamedGroups(t *testing.T) {
	res := Evaluate(`(?P<file>[^:]+):(?P<line>\d+):(.*)`, "main.go:42: boom")
	if len(res.Matches) != 1 {
		t.Fatalf("Evaluate() = %d matches, want 1", len(res.Matches))
	}
	groups := res.Matches[0].Groups
	want := []struct{ name, value string }{
		{"", "main.go:42: boom"},
		{"file", "main.go"},
		{"line", "42"},
		{"", " boom"},
	}
	if len(groups) != len(want) {
		t.Fatalf("got %d groups, want %d", len(groups), len(want))
	}
	for i, w := range want {
		if groups[i].Name != w.name || groups[i].Value != w.value {
			t.Errorf("group %d = %q/%q, want %q/%q", i, groups[i].Name, groups[i].Value, w.name, w.value)
		}
		if !groups[i].Set {
			t.Errorf("group %d reported unset, want set", i)
		}
	}
}

// TestEvaluateUnsetGroup: a group that did not participate is marked unset,
// which is not the same as matching the empty string.
func TestEvaluateUnsetGroup(t *testing.T) {
	res := Evaluate(`(a)|(b)`, "b")
	if len(res.Matches) != 1 {
		t.Fatalf("Evaluate() = %d matches, want 1", len(res.Matches))
	}
	groups := res.Matches[0].Groups
	if groups[1].Set {
		t.Errorf("group 1 reported set, want unset")
	}
	if !groups[2].Set || groups[2].Value != "b" {
		t.Errorf("group 2 = %q (set=%v), want %q set", groups[2].Value, groups[2].Set, "b")
	}

	empty := Evaluate(`a(x*)`, "a")
	if g := empty.Matches[0].Groups[1]; !g.Set || g.Value != "" {
		t.Errorf("empty-matching group = %q (set=%v), want set and empty", g.Value, g.Set)
	}
}

// TestEvaluateInvalidPattern: a compile error is reported, without matches
// and without the "error parsing regexp" preamble.
func TestEvaluateInvalidPattern(t *testing.T) {
	res := Evaluate(`(unclosed`, "text")
	if res.Err == "" {
		t.Fatal("Evaluate() reported no error for an unclosed group")
	}
	if strings.HasPrefix(res.Err, "error parsing regexp") {
		t.Errorf("Evaluate() error = %q, want the preamble trimmed", res.Err)
	}
	if len(res.Matches) != 0 {
		t.Errorf("Evaluate() returned %d matches for an invalid pattern", len(res.Matches))
	}
}

// TestEvaluateEmptyPatternIsIdle: an empty pattern is neither an error nor a
// match at every position — the tester is simply waiting for input.
func TestEvaluateEmptyPatternIsIdle(t *testing.T) {
	res := Evaluate("", "some text")
	if res.Err != "" || len(res.Matches) != 0 {
		t.Errorf("Evaluate(\"\") = %d matches, err %q; want idle", len(res.Matches), res.Err)
	}
}

// TestEvaluateInlineFlags: the RE2 inline flags the spec calls out behave as
// documented — (?i) case-insensitive, (?m) per-line anchors, (?s) dot matches
// the newline.
func TestEvaluateInlineFlags(t *testing.T) {
	if res := Evaluate(`(?i)go`, "Go GO go"); len(res.Matches) != 3 {
		t.Errorf("(?i) matched %d times, want 3", len(res.Matches))
	}
	if res := Evaluate(`(?m)^b`, "a\nb\nb"); len(res.Matches) != 2 {
		t.Errorf("(?m) matched %d times, want 2", len(res.Matches))
	}
	if res := Evaluate(`(?s)a.b`, "a\nb"); len(res.Matches) != 1 {
		t.Errorf("(?s) matched %d times, want 1", len(res.Matches))
	}
	if res := Evaluate(`a.b`, "a\nb"); len(res.Matches) != 0 {
		t.Errorf("dot matched a newline without (?s)")
	}
}

// TestEvaluateTruncates: a pattern matching everywhere stops at the cap and
// says so.
func TestEvaluateTruncates(t *testing.T) {
	res := Evaluate(`a`, strings.Repeat("a", MaxMatches+10))
	if !res.Truncated {
		t.Error("Evaluate() did not report truncation past the cap")
	}
	if len(res.Matches) != MaxMatches {
		t.Errorf("Evaluate() kept %d matches, want the %d cap", len(res.Matches), MaxMatches)
	}
}

// TestLineSpansMapsOffsetsToColumns: spans are per line, in rune columns, and
// a match crossing a line break splits into one span per line.
func TestLineSpansMapsOffsetsToColumns(t *testing.T) {
	text := "über foo\nbar foo baz"
	res := Evaluate(`foo`, text)
	spans := LineSpans(text, res.Matches)
	if len(spans) != 2 {
		t.Fatalf("LineSpans() = %d spans, want 2", len(spans))
	}
	// "über" is 4 runes but 5 bytes: the column must count runes.
	if spans[0] != (Span{Line: 0, Start: 5, End: 8, Match: 0}) {
		t.Errorf("first span = %+v, want line 0 cols 5-8", spans[0])
	}
	if spans[1] != (Span{Line: 1, Start: 4, End: 7, Match: 1}) {
		t.Errorf("second span = %+v, want line 1 cols 4-7", spans[1])
	}

	multi := Evaluate(`(?s)a.*b`, "xa1\n2bx")
	spans = LineSpans("xa1\n2bx", multi.Matches)
	if len(spans) != 2 {
		t.Fatalf("multi-line match = %d spans, want 2", len(spans))
	}
	if spans[0] != (Span{Line: 0, Start: 1, End: 3}) || spans[1] != (Span{Line: 1, Start: 0, End: 2}) {
		t.Errorf("multi-line spans = %+v, want cols 1-3 then 0-2", spans)
	}
}

// TestLineSpansSkipsEmptyMatches: a zero-width match colors nothing (but is
// still counted by the caller).
func TestLineSpansSkipsEmptyMatches(t *testing.T) {
	text := "ab"
	res := Evaluate(`x*`, text)
	if len(res.Matches) == 0 {
		t.Fatal("expected zero-width matches")
	}
	if spans := LineSpans(text, res.Matches); len(spans) != 0 {
		t.Errorf("LineSpans() = %+v, want none for zero-width matches", spans)
	}
}

// TestQuote covers the four copy formats, including the fallbacks.
func TestQuote(t *testing.T) {
	cases := []struct {
		pattern string
		format  QuoteFormat
		want    string
	}{
		{`\d+`, QuoteGoRaw, "`\\d+`"},
		{`\d+`, QuoteGo, `"\\d+"`},
		{`\d+`, QuoteTOML, `'\d+'`},
		{`\d+`, QuoteJSON, `"\\d+"`},
		// A backtick cannot live in a raw string: fall back to the quoted form.
		{"a`b", QuoteGoRaw, "\"a`b\""},
		// A single quote cannot live in a TOML literal string.
		{`it's`, QuoteTOML, `"it's"`},
	}
	for _, c := range cases {
		if got := Quote(c.pattern, c.format); got != c.want {
			t.Errorf("Quote(%q, %v) = %s, want %s", c.pattern, c.format, got, c.want)
		}
	}
}

// TestHistoryDedupesAndCaps: repeats move to the front, the list stays newest
// first and bounded.
func TestHistoryDedupesAndCaps(t *testing.T) {
	var h History
	h.Add("a")
	h.Add("b")
	h.Add("a")
	h.Add("")
	if h.Len() != 2 {
		t.Fatalf("History.Len() = %d, want 2", h.Len())
	}
	if p, _ := h.At(0); p != "a" {
		t.Errorf("newest = %q, want a", p)
	}
	if p, _ := h.At(1); p != "b" {
		t.Errorf("older = %q, want b", p)
	}
	if _, ok := h.At(2); ok {
		t.Error("At() past the end reported ok")
	}
	for i := 0; i < HistoryLimit+10; i++ {
		h.Add(strings.Repeat("x", i+1))
	}
	if h.Len() != HistoryLimit {
		t.Errorf("History.Len() = %d, want the %d cap", h.Len(), HistoryLimit)
	}
}

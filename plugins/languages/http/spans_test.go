package langhttp

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

// spanAt returns the first span covering (line, col), like the editor's
// CaptureAt would resolve it after the overlay prepend.
func spanAt(spans []lang.Span, line, col int) (lang.Span, bool) {
	for _, s := range spans {
		if s.Line == line && col >= s.StartCol && col < s.EndCol {
			return s, true
		}
	}
	return lang.Span{}, false
}

func captureAt(spans []lang.Span, line, col int) string {
	s, _ := spanAt(spans, line, col)
	return s.Capture
}

// TestQuerySpansRequestLine: keys, values and separators of a request-line
// query get their own captures (#1585 items 1–2).
func TestQuerySpansRequestLine(t *testing.T) {
	line := "GET https://api.example.com/search?q=term&limit=10 HTTP/1.1"
	spans := querySpans([]string{line})

	col := func(sub string) int { return strings.Index(line, sub) }
	for _, tc := range []struct {
		sub  string
		want string
	}{
		{"?", "punctuation"},
		{"&", "punctuation"},
		{"q=", "property"},   // key start
		{"term", "string"},   // value
		{"limit", "property"}, // second key
		{"10 ", "string"},    // second value
	} {
		if got := captureAt(spans, 0, col(tc.sub)); got != tc.want {
			t.Errorf("capture at %q = %q, want %q", tc.sub, got, tc.want)
		}
	}
	if got := captureAt(spans, 0, col("=")); got != "punctuation" {
		t.Errorf("capture at '=' = %q, want punctuation", got)
	}
	// The path before '?' carries no query span.
	if got := captureAt(spans, 0, col("search")); got != "" {
		t.Errorf("path segment got capture %q, want none", got)
	}
}

// TestQuerySpansFoldedLines: indented ?/& continuation lines (#1269) get the
// same treatment, with whitespace around tokens left unstyled.
func TestQuerySpansFoldedLines(t *testing.T) {
	lines := []string{
		"GET https://api.example.com/search",
		"    ?q=term",
		"    & limit = 10",
		"Accept: application/json",
	}
	spans := querySpans(lines)
	if got := captureAt(spans, 1, strings.Index(lines[1], "?")); got != "punctuation" {
		t.Errorf("folded '?' capture = %q", got)
	}
	if got := captureAt(spans, 1, strings.Index(lines[1], "q")); got != "property" {
		t.Errorf("folded key capture = %q", got)
	}
	if got := captureAt(spans, 2, strings.Index(lines[2], "limit")); got != "property" {
		t.Errorf("folded spaced key capture = %q", got)
	}
	if got := captureAt(spans, 2, strings.Index(lines[2], "10")); got != "string" {
		t.Errorf("folded spaced value capture = %q", got)
	}
	// The header line after the fold gets nothing.
	for _, s := range spans {
		if s.Line == 3 {
			t.Fatalf("header line got span %+v", s)
		}
	}
}

// TestQuerySpansPercentEncoding: %XX sequences conceal as their decoded
// character; multi-byte encodings span all their triples (#1585 item 3).
func TestQuerySpansPercentEncoding(t *testing.T) {
	line := "GET https://x.test/p?a=hello%20world&b=%C3%A4"
	spans := querySpans([]string{line})

	sp, ok := spanAt(spans, 0, strings.Index(line, "%20"))
	if !ok || sp.Replace != " " || sp.EndCol-sp.StartCol != 3 {
		t.Errorf("%%20 span = %+v, want 3-col Replace \" \"", sp)
	}
	if sp.Capture != "escape" {
		t.Errorf("%%20 capture = %q, want escape", sp.Capture)
	}
	umlaut := strings.Index(line, "%C3%A4")
	sp, ok = spanAt(spans, 0, umlaut)
	if !ok || sp.Replace != "ä" || sp.StartCol != umlaut || sp.EndCol != umlaut+6 {
		t.Errorf("%%C3%%A4 span = %+v, want 6-col Replace \"ä\"", sp)
	}
}

// TestQuerySpansPercentInvalid: a triple that does not decode to a printable
// rune keeps its escape styling but never conceals — hiding it would lie.
func TestQuerySpansPercentInvalid(t *testing.T) {
	line := "GET https://x.test/p?a=%FF&b=%0A"
	spans := querySpans([]string{line})
	for _, sub := range []string{"%FF", "%0A"} {
		sp, ok := spanAt(spans, 0, strings.Index(line, sub))
		if !ok || sp.Capture != "escape" {
			t.Errorf("%s span = %+v, want escape", sub, sp)
		}
		if sp.Replace != "" {
			t.Errorf("%s must not conceal, got Replace %q", sub, sp.Replace)
		}
	}
}

// TestQuerySpansPlaceholdersSkipped: {{…}} and ${…} regions keep the
// grammar's own captures — no overlay span may cover them.
func TestQuerySpansPlaceholdersSkipped(t *testing.T) {
	line := "GET https://x.test/p?a={{host}}&b=${NAME}&c=1"
	spans := querySpans([]string{line})
	for _, sub := range []string{"{{host}}", "${NAME}"} {
		start := strings.Index(line, sub)
		for col := start; col < start+len(sub); col++ {
			if s, ok := spanAt(spans, 0, col); ok {
				t.Fatalf("placeholder col %d covered by %+v", col, s)
			}
		}
	}
	if got := captureAt(spans, 0, strings.Index(line, "c=")); got != "property" {
		t.Errorf("key after placeholders = %q, want property", got)
	}
}

// TestQuerySpansFormBody: an application/x-www-form-urlencoded body gets the
// same key/value/percent treatment; other bodies stay untouched.
func TestQuerySpansFormBody(t *testing.T) {
	lines := []string{
		"POST https://x.test/submit",
		"Content-Type: application/x-www-form-urlencoded",
		"",
		"name=J%C3%BCrgen&city=K%C3%B6ln",
	}
	spans := querySpans(lines)
	if got := captureAt(spans, 3, 0); got != "property" {
		t.Errorf("body key capture = %q, want property", got)
	}
	sp, ok := spanAt(spans, 3, strings.Index(lines[3], "%C3%BC"))
	if !ok || sp.Replace != "ü" {
		t.Errorf("body percent span = %+v, want Replace \"ü\"", sp)
	}

	json := []string{
		"POST https://x.test/submit",
		"Content-Type: application/json",
		"",
		`{"a": "b=c&d=e"}`,
	}
	for _, s := range querySpans(json) {
		if s.Line == 3 {
			t.Fatalf("json body got span %+v", s)
		}
	}
}

package langhttp

import "testing"

// TestMultipartSpansBoundaryAndHeaders (#2135): boundary delimiters read
// like the grammar's own request separators, and each part's header block
// reads like the grammar's own headers (name @constant, ":" @punctuation,
// value @property) — the part body itself stays unstyled.
func TestMultipartSpansBoundaryAndHeaders(t *testing.T) {
	lines := []string{
		"POST https://api.test/upload",                               // 0
		"Content-Type: multipart/form-data; boundary=WebAppBoundary", // 1
		"",                 // 2
		"--WebAppBoundary", // 3
		`Content-Disposition: form-data; name="field1"`, // 4
		"",                   // 5
		"value1",             // 6
		"--WebAppBoundary--", // 7
	}
	spans := querySpans(lines)

	for _, tc := range []struct {
		line int
		want string
	}{
		{3, "comment"}, // opening boundary
		{7, "comment"}, // closing boundary
	} {
		if got := captureAt(spans, tc.line, 0); got != tc.want {
			t.Errorf("line %d capture = %q, want %q", tc.line, got, tc.want)
		}
	}

	if got := captureAt(spans, 4, 0); got != "constant" {
		t.Errorf("part header name capture = %q, want constant", got)
	}
	colon := len("Content-Disposition")
	if got := captureAt(spans, 4, colon); got != "punctuation" {
		t.Errorf("part header colon capture = %q, want punctuation", got)
	}
	if got := captureAt(spans, 4, colon+2); got != "property" {
		t.Errorf("part header value capture = %q, want property", got)
	}

	if _, ok := spanAt(spans, 6, 0); ok {
		t.Errorf("part body must stay unstyled, got a span at line 6")
	}
}

// TestMultipartSpansPlaceholderWinsInHeaderValue: a "{{field}}" placeholder
// inside a part header value keeps its own punctuation+variable styling
// rather than reading as one flat @property value (#1880 stays intact).
func TestMultipartSpansPlaceholderWinsInHeaderValue(t *testing.T) {
	lines := []string{
		"POST https://api.test/upload",
		"Content-Type: multipart/form-data; boundary=b",
		"",
		"--b",
		`Content-Disposition: form-data; name="{{field}}"`,
		"",
		"value1",
		"--b--",
	}
	spans := querySpans(lines)
	start := len(`Content-Disposition: form-data; name="`)
	if got := captureAt(spans, 4, start); got != "punctuation" {
		t.Errorf("placeholder opening brace = %q, want punctuation", got)
	}
	if got := captureAt(spans, 4, start+2); got != "variable" {
		t.Errorf("placeholder name = %q, want variable", got)
	}
}

// TestMultipartSpansIgnoreNonMultipartBodies: a JSON body gets no boundary
// or header-block styling even if it happens to contain a "--" line.
func TestMultipartSpansIgnoreNonMultipartBodies(t *testing.T) {
	lines := []string{
		"POST https://api.test/x",
		"Content-Type: application/json",
		"",
		`{"note": "--not-a-boundary"}`,
	}
	if got := multipartSpans(lines); len(got) != 0 {
		t.Errorf("multipartSpans(json body) = %+v, want none", got)
	}
}

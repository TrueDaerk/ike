package langhttp

import (
	"testing"
)

// assert_test.go covers the editor side of the `# @assert` directive (#2546):
// the highlighting of its parts and the completion of its keywords.

// TestAssertDirectiveHighlighted: marker, subject, argument, operator and
// expected value are told apart; the comment marker stays with the grammar.
func TestAssertDirectiveHighlighted(t *testing.T) {
	lines := []string{
		//        1         2         3         4
		// 01234567890123456789012345678901234567890123
		`# @assert header Content-Type contains json`,
		`# @assert jsonpath $.items[0].id == 42`,
		`# @assert body matches /"ok":\s*true/`,
		`# @assert nonsense`,
		`GET https://example.com/things`,
	}
	cases := []struct {
		line, col int
		want      string
		what      string
	}{
		{0, 0, "", "the comment marker"},
		{0, 2, "keyword", "@assert marker"},
		{0, 10, "type", "the subject"},
		{0, 17, "property", "the header name"},
		{0, 30, "operator", "contains"},
		{0, 39, "string", "the expected text"},
		{1, 19, "property", "the JSONPath"},
		{1, 33, "operator", "=="},
		{1, 36, "number", "a numeric expected value"},
		{2, 15, "operator", "matches"},
		{2, 23, "string", "the regex, delimiters included"},
		{3, 2, "keyword", "a broken directive's marker"},
		{3, 10, "", "a broken directive's rest"},
	}
	for _, c := range cases {
		if got := spanCaptureAt(t, lines, c.line, c.col); got != c.want {
			t.Errorf("%s (line %d col %d): capture %q, want %q", c.what, c.line, c.col, got, c.want)
		}
	}
}

// The directive marker completes on a `# @` line, with the space that
// follows it; a plain comment still completes nothing.
func TestDirectiveMarkerCompletes(t *testing.T) {
	items := completeAt(t, "### x\n# @|\nGET https://example.com\n")
	if !has(items, "@assert") || !has(items, "@capture") {
		t.Fatalf("items = %v, want both directives", labels(items))
	}
	if got := insertFor(items, "@assert"); got != "@assert " {
		t.Errorf("insert = %q, want the marker plus its space", got)
	}
	items = completeAt(t, "### x\n// @as|\nGET https://example.com\n")
	if !has(items, "@assert") || has(items, "@capture") {
		t.Errorf("typed prefix must filter: %v", labels(items))
	}
	if items := completeAt(t, "### x\n# just a comment |\nGET https://example.com\n"); len(items) != 0 {
		t.Errorf("a plain comment completes nothing, got %v", labels(items))
	}
}

// Inside an assertion the subject, the header name and the operator complete
// in turn, each where it belongs.
func TestAssertDirectiveCompletes(t *testing.T) {
	cases := []struct {
		src       string
		want      string
		wantNot   string
		wantEmpty bool
	}{
		{src: "# @assert |", want: "status", wantNot: "=="},
		{src: "# @assert js|", want: "jsonpath", wantNot: "status"},
		{src: "# @assert status |", want: "==", wantNot: "status"},
		{src: "# @assert status co|", want: "contains", wantNot: "=="},
		{src: "# @assert time |", want: "<", wantNot: "header"},
		{src: "# @assert body |", want: "matches"},
		{src: "# @assert header |", want: "Content-Type", wantNot: "=="},
		{src: "# @assert header Content-Type |", want: "contains", wantNot: "Content-Type"},
		{src: "# @assert jsonpath $.id |", want: "==", wantNot: "status"},
		{src: "# @assert jsonpath |", wantEmpty: true},
		{src: "# @assert status == |", wantEmpty: true},
		{src: "# @assert header Content-Type contains |", wantEmpty: true},
	}
	for _, c := range cases {
		items := completeAt(t, "### x\n"+c.src+"\nGET https://example.com\n")
		if c.wantEmpty {
			if len(items) != 0 {
				t.Errorf("%q: want nothing, got %v", c.src, labels(items))
			}
			continue
		}
		if !has(items, c.want) {
			t.Errorf("%q: want %q among %v", c.src, c.want, labels(items))
		}
		if c.wantNot != "" && has(items, c.wantNot) {
			t.Errorf("%q: %q must not be offered (%v)", c.src, c.wantNot, labels(items))
		}
	}
	// The header name inserts bare — no ": " separator, this is not a header
	// line.
	items := completeAt(t, "### x\n# @assert header Con|\nGET https://example.com\n")
	if got := insertFor(items, "Content-Type"); got != "Content-Type" {
		t.Errorf("insert = %q, want the bare name", got)
	}
}

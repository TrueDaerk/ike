package langhttp

import "testing"

// capture_test.go covers the highlighting of the `# @capture` directive
// (#1993). The spans are Go-computed, so they can be asserted without the
// grammar (and therefore without cgo).

// captureAt returns the capture the span producer assigns to one cell, "" for
// a cell it leaves to the grammar. Later spans never win, matching the
// first-covering-wins lookup the highlighter does.
func spanCaptureAt(t *testing.T, lines []string, line, col int) string {
	t.Helper()
	for _, s := range querySpans(lines) {
		if s.Line == line && col >= s.StartCol && col < s.EndCol {
			return s.Capture
		}
	}
	return ""
}

// TestCaptureDirectiveHighlighted: the directive reads as structure, not as
// one flat comment — marker, name, "=" and the jq expression are told apart.
func TestCaptureDirectiveHighlighted(t *testing.T) {
	lines := []string{
		//        1         2         3
		// 012345678901234567890123456789012345
		`# @capture token = .data.access_token`,
		`POST https://example.com/oauth/token`,
	}
	cases := []struct {
		col  int
		want string
		what string
	}{
		{2, "keyword", "@capture marker"},
		{11, "variable", "variable name"},
		{17, "operator", "="},
		{19, "property", "the jq path"},
	}
	for _, c := range cases {
		if got := spanCaptureAt(t, lines, 0, c.col); got != c.want {
			t.Errorf("%s (col %d): capture %q, want %q", c.what, c.col, got, c.want)
		}
	}
	// The "#" itself keeps the grammar's comment colour — the line *is* a
	// comment, and only its meaningful parts are lifted out of it.
	if got := spanCaptureAt(t, lines, 0, 0); got != "" {
		t.Errorf("the comment marker must be left to the grammar, got %q", got)
	}
}

// TestCaptureExpressionTokens: the expression runs through the jq tokenizer,
// so a string literal, a builtin and an operator inside it read as themselves.
func TestCaptureExpressionTokens(t *testing.T) {
	//                  1         2         3         4
	//        0123456789012345678901234567890123456789012
	line := `# @capture id = .items | map(select(.a=="b")) | first | .id`
	lines := []string{line, "GET https://example.com/x"}
	want := map[int]string{
		16: "property",                            // .items
		23: "punctuation" /* | */, 25: "function", // map
		36: "property", // the .a path inside select()
		40: "string",   // the "b" literal it compares against
	}
	for col, capture := range want {
		if got := spanCaptureAt(t, lines, 0, col); got != capture {
			t.Errorf("col %d (%q): capture %q, want %q", col, string([]rune(line)[col]), got, capture)
		}
	}
}

// TestNonDirectiveCommentsUntouched: an ordinary comment and the "###"
// separator (which the parser reads as a request name, not a directive) get
// no capture spans at all.
func TestNonDirectiveCommentsUntouched(t *testing.T) {
	lines := []string{
		"# just a note about @capture usage",
		"### @capture id = .id",
		"GET https://example.com/x",
	}
	for _, s := range captureSpans(lines) {
		t.Errorf("unexpected span %+v", s)
	}
}

// TestFormatKeepsCaptureDirectives: the reformatter leaves a directive
// verbatim — it is a comment, and its spelling is the author's — while still
// normalizing the request below it.
func TestFormatKeepsCaptureDirectives(t *testing.T) {
	src := "# @capture task = .task\nGET https://example.com/x?a=1\nAccept:*/*\n"
	got, err := formatHTTP(src, "    ")
	if err != nil {
		t.Fatal(err)
	}
	want := "# @capture task = .task\nGET https://example.com/x\n    ? a = 1\nAccept: */*\n"
	if got != want {
		t.Errorf("formatted:\n%q\nwant:\n%q", got, want)
	}
}

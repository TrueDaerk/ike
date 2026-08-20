package httpfile

import "testing"

// capture_test.go covers the parser side of the capture directive (#1993):
// which comment lines become a Capture, where they attach, and what they
// carry. Evaluating them is internal/httpclient's job and is tested there.

// TestCaptureDirectiveSpellings: the forms recognised (and the near-misses
// that must stay ordinary comments).
func TestCaptureDirectiveSpellings(t *testing.T) {
	ok := map[string][2]string{
		"# @capture token = .access_token":   {"token", ".access_token"},
		"#@capture token=.access_token":      {"token", ".access_token"},
		"// @capture task.id = .task":        {"task.id", ".task"},
		"## @capture id = .id":               {"id", ".id"},
		"   #  @capture  a  =  .b | .c  ":    {"a", ".b | .c"},
		`# @capture q = .hits[0]._source.id`: {"q", ".hits[0]._source.id"},
	}
	for line, want := range ok {
		name, expr, got := CaptureDirective(line)
		if !got || name != want[0] || expr != want[1] {
			t.Errorf("CaptureDirective(%q) = %q,%q,%v; want %q,%q,true", line, name, expr, got, want[0], want[1])
		}
	}
	for _, line := range []string{
		"# capture token = .a",     // no @
		"@capture token = .a",      // not a comment: an @name=value definition
		"# @capture token =",       // no expression
		"# @capture = .a",          // no name
		"# @capture 1token = .a",   // name must start like an identifier
		"# @captured token = .a",   // different word
		"GET https://x/@capture=1", // not a comment line
		"# @capture token .a",      // no =
		"### @capture id = .id",    // a separator line, not a comment
	} {
		if name, expr, ok := CaptureDirective(line); ok {
			t.Errorf("CaptureDirective(%q) matched as %q = %q", line, name, expr)
		}
	}
}

// TestCaptureAttachesToRequest: directives above the request line, between
// folded query lines and inside the header block all belong to the block's
// request, in file order, with their position recorded.
func TestCaptureAttachesToRequest(t *testing.T) {
	src := "### start\n" +
		"# @capture task = .task\n" +
		"POST https://example.com/reindex\n" +
		"  ? wait = false\n" +
		"// @capture node = .node\n" +
		"Accept: application/json\n" +
		"# @capture total = .total\n" +
		"\n" +
		"{\"# @capture body = .nope\": 1}\n"
	f := Parse(src)
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("parse: errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	got := f.Requests[0].Captures
	want := []Capture{
		{Name: "task", Expr: ".task", Line: 2, EndCol: 23},
		{Name: "node", Expr: ".node", Line: 5, EndCol: 24},
		{Name: "total", Expr: ".total", Line: 7, EndCol: 25},
	}
	if len(got) != len(want) {
		t.Fatalf("captures: %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("capture %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestCapturePerRequest: every request keeps its own directives — the one in
// the second block must not leak into the first.
func TestCapturePerRequest(t *testing.T) {
	f := Parse("# @capture a = .a\nGET https://example.com/1\n\n### second\n# @capture b = .b\nGET https://example.com/2\n")
	if len(f.Requests) != 2 {
		t.Fatalf("requests=%d errors=%v", len(f.Requests), f.Errors)
	}
	if len(f.Requests[0].Captures) != 1 || f.Requests[0].Captures[0].Name != "a" {
		t.Errorf("first request: %+v", f.Requests[0].Captures)
	}
	if len(f.Requests[1].Captures) != 1 || f.Requests[1].Captures[0].Name != "b" {
		t.Errorf("second request: %+v", f.Requests[1].Captures)
	}
}

// TestCaptureNoneIsNil: a file without directives keeps the field empty
// rather than allocating — the overwhelmingly common case.
func TestCaptureNoneIsNil(t *testing.T) {
	f := Parse("# an ordinary comment\nGET https://example.com/\n")
	if len(f.Requests) != 1 {
		t.Fatalf("requests=%d", len(f.Requests))
	}
	if f.Requests[0].Captures != nil {
		t.Errorf("captures: %+v, want nil", f.Requests[0].Captures)
	}
}

package httpfile

import (
	"strings"
	"testing"
)

// assert_test.go covers the `# @assert` directive (#2546): what parses, what
// each part means, and that a broken directive is kept with its reason
// rather than dropped.

func TestAssertDirectiveParses(t *testing.T) {
	cases := []struct {
		line string
		want Assertion
	}{
		{"# @assert status == 200", Assertion{Subject: "status", Op: "==", Expected: "200"}},
		{"// @assert status != 500", Assertion{Subject: "status", Op: "!=", Expected: "500"}},
		{"## @assert status < 400", Assertion{Subject: "status", Op: "<", Expected: "400"}},
		{"#@assert header Content-Type contains json", Assertion{Subject: "header", Arg: "Content-Type", Op: "contains", Expected: "json"}},
		{"# @assert header X-Trace exists", Assertion{Subject: "header", Arg: "X-Trace", Op: "exists"}},
		{"# @assert header Content-Type == application/json; charset=utf-8",
			Assertion{Subject: "header", Arg: "Content-Type", Op: "==", Expected: "application/json; charset=utf-8"}},
		{"# @assert jsonpath $.items[0].id == 42", Assertion{Subject: "jsonpath", Arg: "$.items[0].id", Op: "==", Expected: "42"}},
		{"# @assert jsonpath $.items exists", Assertion{Subject: "jsonpath", Arg: "$.items", Op: "exists"}},
		{`# @assert body matches /"ok":\s*true/`, Assertion{Subject: "body", Op: "matches", Expected: `"ok":\s*true`}},
		{"# @assert body contains 'hello world'", Assertion{Subject: "body", Op: "contains", Expected: "hello world"}},
		{`# @assert body == "quoted"`, Assertion{Subject: "body", Op: "==", Expected: "quoted"}},
		{"  #   @assert   time   <   500ms   ", Assertion{Subject: "time", Op: "<", Expected: "500ms"}},
	}
	for _, c := range cases {
		got, ok := AssertDirective(c.line)
		if !ok {
			t.Errorf("%q: not recognised", c.line)
			continue
		}
		if got.Err != "" {
			t.Errorf("%q: unexpected error %q", c.line, got.Err)
		}
		got.Raw = ""
		if got != c.want {
			t.Errorf("%q:\n got %+v\nwant %+v", c.line, got, c.want)
		}
	}
}

func TestAssertDirectiveRejectsNonDirectives(t *testing.T) {
	for _, line := range []string{
		"### @assert status == 200", // a separator, never a directive
		"# @capture id = .id",
		"# assert status == 200",
		"@assert status == 200", // not a comment
		"GET https://example.com",
	} {
		if _, ok := AssertDirective(line); ok {
			t.Errorf("%q must not read as an assertion", line)
		}
	}
}

// A directive that is recognised but does not parse is kept with the reason:
// the response pane reports it as a failure instead of the file losing it.
func TestAssertDirectiveKeepsBrokenOnes(t *testing.T) {
	cases := []struct{ line, want string }{
		{"# @assert", "missing subject"},
		{"# @assert code == 200", `unknown subject "code"`},
		{"# @assert status equals 200", `unknown operator "equals"`},
		{"# @assert status ==", "missing expected value"},
		{"# @assert header", "missing header name"},
		{"# @assert jsonpath", "missing JSONPath"},
		{"# @assert status", "missing operator"},
		{"# @assert header X exists yes", "takes no value"},
		{"# @assert status == OK", "compares with a number"},
	}
	for _, c := range cases {
		got, ok := AssertDirective(c.line)
		if !ok {
			t.Errorf("%q: not recognised", c.line)
			continue
		}
		if !strings.Contains(got.Err, c.want) {
			t.Errorf("%q: err %q, want it to mention %q", c.line, got.Err, c.want)
		}
	}
}

func TestAssertionString(t *testing.T) {
	cases := []struct {
		a    Assertion
		want string
	}{
		{Assertion{Subject: "status", Op: "==", Expected: "200"}, "status == 200"},
		{Assertion{Subject: "header", Arg: "X-Id", Op: "exists"}, "header X-Id exists"},
		{Assertion{Subject: "jsonpath", Arg: "$.a", Op: "!=", Expected: "b c"}, "jsonpath $.a != b c"},
		{Assertion{Raw: "nonsense here", Err: "unknown subject"}, "nonsense here"},
	}
	for _, c := range cases {
		if got := c.a.String(); got != c.want {
			t.Errorf("%+v: String() = %q, want %q", c.a, got, c.want)
		}
	}
}

// The parser attaches the directives to the block they sit in — anywhere a
// comment goes, before or after the request line, but not in a body — and
// records where each one is.
func TestParseCollectsAssertions(t *testing.T) {
	src := strings.Join([]string{
		"### first",
		"# @assert status == 200",
		"GET https://example.com/a",
		"# @assert header Content-Type contains json",
		"Accept: */*",
		"",
		"# @assert body contains not-a-directive-inside-a-body",
		"### second",
		"POST https://example.com/b",
		"# @assert time < 1s",
		"# @assert bogus",
	}, "\n")
	f := Parse(src)
	if len(f.Errors) != 0 {
		t.Fatalf("errors: %v", f.Errors)
	}
	if len(f.Requests) != 2 {
		t.Fatalf("requests: %d", len(f.Requests))
	}
	first := f.Requests[0].Assertions
	if len(first) != 2 {
		t.Fatalf("first request assertions: %+v", first)
	}
	if first[0].Line != 2 || first[0].EndCol != len("# @assert status == 200") {
		t.Errorf("first assertion located at %d:%d", first[0].Line, first[0].EndCol)
	}
	if first[1].Subject != "header" || first[1].Line != 4 {
		t.Errorf("second assertion = %+v", first[1])
	}
	if strings.Contains(f.Requests[0].Body, "@assert") == false {
		t.Errorf("the body line must stay body text, got %q", f.Requests[0].Body)
	}
	second := f.Requests[1].Assertions
	if len(second) != 2 || second[0].Subject != "time" || second[1].Err == "" {
		t.Fatalf("second request assertions: %+v", second)
	}
	if !f.Asserting() {
		t.Error("Asserting() must report the directives")
	}
	if Parse("GET https://example.com").Asserting() {
		t.Error("a file without directives is not asserting")
	}
}

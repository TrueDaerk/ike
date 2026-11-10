package httpfile

import (
	"strings"
	"testing"
)

// nolog_test.go covers the `# @no-log` directive (#3115).

func TestNoLogDirectiveSpellings(t *testing.T) {
	for _, line := range []string{"# @no-log", "## @no-log", "// @no-log", "   #   @no-log   "} {
		if err, ok := NoLogDirectiveAt(line); !ok || err != nil {
			t.Errorf("%q: ok=%v err=%v, want the directive", line, ok, err)
		}
	}
	for _, line := range []string{
		"### @no-log",
		"# no-log",
		"# @no-logs",
		"# @no-log-file",
		"GET http://example.test/@no-log",
	} {
		if _, ok := NoLogDirectiveAt(line); ok {
			t.Errorf("%q must not be a directive", line)
		}
	}
	if err, ok := NoLogDirectiveAt("# @no-log something"); !ok || err == nil {
		t.Errorf("a value must be an error: ok=%v err=%v", ok, err)
	}
}

// The flag attaches to its own block only, in every place a comment may
// stand; `### @no-log` opens a block instead.
func TestParseNoLogPerBlock(t *testing.T) {
	src := strings.Join([]string{
		"### a",
		"# @no-log",
		"GET http://example.test/a",
		"",
		"### b",
		"GET http://example.test/b",
		"",
		"### c",
		"GET http://example.test/c",
		"// @no-log",
		"Accept: */*",
		"",
		"### d",
		"## @no-log",
		"GET http://example.test/d",
		"",
		"### @no-log",
		"GET http://example.test/e",
	}, "\n")
	f := Parse(src)
	if len(f.Errors) != 0 || len(f.Requests) != 5 {
		t.Fatalf("errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	want := []bool{true, false, true, true, false}
	for i, w := range want {
		if f.Requests[i].NoLog != w {
			t.Errorf("request %d NoLog = %v, want %v", i, f.Requests[i].NoLog, w)
		}
	}
}

// A value after the marker is reported on the directive's line and drops the
// block, exactly like a bad `@timeout`; the next block is unaffected.
func TestParseNoLogValueIsError(t *testing.T) {
	f := Parse("### a\n# @no-log something\nGET http://example.test/a\n\n### b\nGET http://example.test/b\n")
	if len(f.Errors) != 1 || f.Errors[0].Line != 2 || !strings.Contains(f.Errors[0].Msg, "@no-log takes no value") {
		t.Fatalf("errors = %v, want one on line 2", f.Errors)
	}
	if len(f.Requests) != 1 || f.Requests[0].Name != "b" || f.Requests[0].NoLog {
		t.Fatalf("want only block b without the flag, got %+v", f.Requests)
	}
}
